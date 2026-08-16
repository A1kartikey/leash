package chain

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ---------------------------------------------------------------------------
// Nonce serialisation
//
// Each signer is owned by exactly one goroutine that holds its nonce and
// submits sequentially from a channel. Callers send a txRequest and wait on
// a reply channel. No other code path may sign for that signer.
//
// On a nonce-too-low error the loop re-syncs from
// eth_getTransactionCount(pending) and retries the request once.
// ---------------------------------------------------------------------------

// txRequest is what callers push into the signer loop.
type txRequest struct {
	// build constructs and sends the transaction using the given opts.
	// The opts already have Nonce, Signer, From, ChainID, GasPrice set.
	// The build func only adds method-specific fields (Value, etc.).
	build func(opts *bind.TransactOpts) (*types.Transaction, error)

	// reply receives the outcome.
	reply chan txResult
}

// txResult is what the signer loop sends back.
type txResult struct {
	tx  *types.Transaction
	err error
}

// signerState is the mutable state owned exclusively by the signer goroutine.
type signerState struct {
	mu      sync.Mutex // guards only the shutdown path
	nonce   uint64
	key     *ecdsa.PrivateKey
	chainID *big.Int
	client  *ethclient.Client
	from    [20]byte // common.Address stored as array for quick comparison
}

// signerLoop is the single-writer goroutine for a signer. It processes
// txRequests sequentially, ensuring nonce monotonicity.
//
// The goroutine exits when ctx is cancelled or txReqCh is closed.
func signerLoop(ctx context.Context, client *ethclient.Client, key *ecdsa.PrivateKey, chainID *big.Int, txReqCh <-chan txRequest) {
	from := crypto.PubkeyToAddress(key.PublicKey)

	// Seed nonce from the network once at startup.
	nonce, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		// If we can't get the initial nonce, every request will fail with a
		// descriptive error until the context is cancelled.
		drainWithError(txReqCh, fmt.Errorf("initial nonce fetch: %w", err))
		return
	}

	signer := types.LatestSignerForChainID(chainID)

	for {
		select {
		case <-ctx.Done():
			return
		case req, ok := <-txReqCh:
			if !ok {
				return
			}

			opts := &bind.TransactOpts{
				From: from,
				Nonce: new(big.Int).SetUint64(nonce),
				Signer: func(addr common.Address, tx *types.Transaction) (*types.Transaction, error) {
					if addr != from {
						return nil, fmt.Errorf("signer: asked to sign for %s but own %s", addr, from)
					}
					return types.SignTx(tx, signer, key)
				},
				Context: ctx,
			}

			tx, txErr := buildWithGasBuffer(req.build, opts)

			if txErr != nil && isNonceTooLow(txErr) {
				// Re-sync nonce and retry once.
				synced, syncErr := client.PendingNonceAt(ctx, from)
				if syncErr == nil {
					nonce = synced
					opts.Nonce = new(big.Int).SetUint64(nonce)
					tx, txErr = buildWithGasBuffer(req.build, opts)
				} else {
					txErr = fmt.Errorf("nonce re-sync failed: %w (original: %w)", syncErr, txErr)
				}
			}

			if txErr == nil {
				nonce++
			}

			req.reply <- txResult{tx: tx, err: txErr}
		}
	}
}

// gasBufferPercent is the headroom added to the estimated gas limit.
//
// go-ethereum's bind uses eth_estimateGas verbatim, and measured against this
// contract the estimate sat 0.8% above the minimum the call needs. That margin
// does not survive concurrency: with 20 locks in flight, 6 of 20 reverted at
// the bare estimate and 1 of 20 still reverted at +10%. Monad prices cold
// storage access at 8,100 gas against Ethereum's 2,100, so a slot the
// estimator saw warm costs 6,000 more when the transaction actually runs —
// a handful of those is exactly the overrun observed.
//
// 25% is measured, not guessed: it is the first buffer at which 20 concurrent
// locks all landed. Monad charges gas_limit * price rather than gas used, so
// headroom is money the buyer pays on every transaction — but a reverted
// transaction is charged its full limit too, and loses the lock as well, so
// erring generous is strictly cheaper than erring tight.
const gasBufferPercent = 25

// buildWithGasBuffer builds the transaction twice: once without sending, to
// learn the gas bind would have used, then again with that estimate plus
// headroom. The first pass is where the estimate happens, so this costs no
// extra RPC round trip beyond the one bind already made.
func buildWithGasBuffer(build func(*bind.TransactOpts) (*types.Transaction, error), opts *bind.TransactOpts) (*types.Transaction, error) {
	// Clear any limit left by a previous attempt, or the probe would just
	// re-use it instead of estimating.
	opts.GasLimit = 0
	opts.NoSend = true
	probe, err := build(opts)
	opts.NoSend = false
	if err != nil {
		// Sending would fail identically; report the error the caller expects.
		return nil, err
	}

	opts.GasLimit = probe.Gas() * (100 + gasBufferPercent) / 100
	return build(opts)
}

// submitTx sends a transaction request through the signer loop and waits for
// the result. This is the only way to create a signed transaction.
func submitTx(ctx context.Context, txReqCh chan<- txRequest, build func(*bind.TransactOpts) (*types.Transaction, error)) (*types.Transaction, error) {
	reply := make(chan txResult, 1)
	req := txRequest{build: build, reply: reply}

	select {
	case txReqCh <- req:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case res := <-reply:
		return res.tx, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// drainWithError responds to all pending requests with the given error.
func drainWithError(ch <-chan txRequest, err error) {
	for req := range ch {
		req.reply <- txResult{err: err}
	}
}

// isNonceTooLow checks whether an error indicates the nonce has already been
// used. go-ethereum and various RPC providers use different message strings.
func isNonceTooLow(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, needle := range []string{
		"nonce too low",
		"nonce has already been used",
		"replacement transaction underpriced",
		"already known",
	} {
		if contains(msg, needle) {
			return true
		}
	}
	return false
}

// contains is a simple case-insensitive substring check.
func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) &&
		// Cheap lowercase comparison for ASCII error strings.
		containsLower(haystack, needle)
}

func containsLower(s, sub string) bool {
	s = toLower(s)
	sub = toLower(sub)
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
