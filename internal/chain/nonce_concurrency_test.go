//go:build integration

// Nonce serialisation under real concurrent load.
//
// Every other test in this suite locks sequentially, which is precisely the
// pattern that cannot fail: one caller, one nonce, one transaction in flight.
// The signer loop in nonce.go exists for the other case — many goroutines
// asking one key to sign at the same instant — and that case is only exercised
// here.
//
// A broken serialiser fails in three distinguishable ways, so the assertions
// below separate them: two transactions signed with the same nonce (one is
// silently replaced), a nonce skipped (a transaction never lands), or a
// caller getting an error back. All three are invisible when locks are
// sequential.
//
// The escrow amount is deliberately dust: this test is about nonces, not
// money, and it deliberately does not refund — on a real chain the gas to
// recover the stake would cost orders of magnitude more than the stake.
//
// Run:
//
//	MONAD_RPC=https://testnet-rpc.monad.xyz BUYER_KEY=<hex> \
//	go test -v -tags integration -timeout 900s -run Concurrent ./internal/chain/...
package chain_test

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/A1kartikey/leash/internal/chain"
	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// concurrentLocks is the load. Twenty is well past the txReqCh buffer of 16,
// so the test also covers callers that have to block waiting for the loop.
const concurrentLocks = 20

// dust is the smallest stake that still creates a real escrow: the contract
// rejects zero, and nothing here depends on the amount.
func dust() *big.Int { return big.NewInt(1_000_000_000) } // 1 gwei

func TestConcurrentLocksSerialiseNonces(t *testing.T) {
	ec, ctx, _ := testEnv(t)

	cfg, err := chain.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	client := ec.Client()

	startNonce, err := client.PendingNonceAt(ctx, cfg.SignerAddr)
	if err != nil {
		t.Fatalf("reading start nonce: %v", err)
	}
	balBefore, err := ec.BalanceOf(ctx, cfg.SignerAddr)
	if err != nil {
		t.Fatalf("reading balance: %v", err)
	}
	t.Logf("signer %s at nonce %d, balance %s wei", cfg.SignerAddr, startNonce, balBefore)

	type outcome struct {
		id  types.EscrowID
		tx  types.TxHash
		err error
	}
	results := make([]outcome, concurrentLocks)

	// Every goroutine blocks on the same channel, so the requests hit the
	// signer loop together rather than trickling in.
	var wg sync.WaitGroup
	gun := make(chan struct{})
	for i := 0; i < concurrentLocks; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-gun
			id, tx, err := ec.Lock(ctx, randomAddress(), dust(), randomHash())
			results[i] = outcome{id: id, tx: tx, err: err}
		}(i)
	}

	began := time.Now()
	close(gun)
	wg.Wait()
	t.Logf("%d concurrent locks completed in %s", concurrentLocks, time.Since(began).Round(time.Millisecond))

	// 1. Every caller got a transaction. A serialiser that drops or errors
	//    under contention fails here first.
	var failed int
	for i, r := range results {
		if r.err != nil {
			failed++
			t.Errorf("lock %d failed: %v %s", i, r.err, diagnose(t, ec, r.tx))
		}
	}
	if failed > 0 {
		t.Fatalf("%d of %d concurrent locks failed", failed, concurrentLocks)
	}

	// 2. Distinct escrows and distinct transactions. Two callers sharing an
	//    escrow id would mean two receipts parsed as one.
	ids := map[types.EscrowID]int{}
	txs := map[types.TxHash]int{}
	for i, r := range results {
		if prev, dup := ids[r.id]; dup {
			t.Fatalf("locks %d and %d both got escrow %d", prev, i, r.id)
		}
		if prev, dup := txs[r.tx]; dup {
			t.Fatalf("locks %d and %d share tx %s", prev, i, r.tx)
		}
		ids[r.id] = i
		txs[r.tx] = i
	}

	// 3. The nonces actually used. This is the assertion the test exists for:
	//    exactly one transaction per nonce, contiguous from where we started.
	//    A collision shows up as a gap plus a missing transaction, because the
	//    replaced one never lands.
	seen := map[uint64]types.TxHash{}
	for _, r := range results {
		tx, _, err := client.TransactionByHash(ctx, common.HexToHash(string(r.tx)))
		if err != nil {
			t.Fatalf("fetching %s: %v — a transaction the signer reported as sent is not on chain", r.tx, err)
		}
		if other, clash := seen[tx.Nonce()]; clash {
			t.Fatalf("nonce %d used by two transactions: %s and %s", tx.Nonce(), other, r.tx)
		}
		seen[tx.Nonce()] = types.TxHash(tx.Hash().Hex())
	}
	for n := startNonce; n < startNonce+concurrentLocks; n++ {
		if _, ok := seen[n]; !ok {
			t.Fatalf("nonce %d was never used: the run is not contiguous from %d, so a transaction was dropped",
				n, startNonce)
		}
	}

	// 4. The account itself advanced by exactly the number of locks — the
	//    chain's own count of what landed, independent of what we recorded.
	endNonce, err := client.PendingNonceAt(ctx, cfg.SignerAddr)
	if err != nil {
		t.Fatalf("reading end nonce: %v", err)
	}
	if endNonce != startNonce+concurrentLocks {
		t.Fatalf("account nonce went %d -> %d, want +%d", startNonce, endNonce, concurrentLocks)
	}

	// 5. Every escrow is really there, locked, holding the stake, ours.
	for i, r := range results {
		e := escrowOnChain(t, ctx, ec.Contract(), r.id)
		if e.Buyer != cfg.SignerAddr {
			t.Fatalf("escrow %d (lock %d) has buyer %s, want %s", r.id, i, e.Buyer, cfg.SignerAddr)
		}
		if e.Status != 0 { // 0 = Locked
			t.Fatalf("escrow %d (lock %d) has status %d, want Locked(0)", r.id, i, e.Status)
		}
		if e.Amount.Cmp(dust()) != 0 {
			t.Fatalf("escrow %d (lock %d) holds %s wei, want %s", r.id, i, e.Amount, dust())
		}
	}

	balAfter, err := ec.BalanceOf(ctx, cfg.SignerAddr)
	if err != nil {
		t.Fatalf("reading balance: %v", err)
	}
	t.Logf("all %d locks landed on nonces %d..%d, escrows %v",
		concurrentLocks, startNonce, startNonce+concurrentLocks-1, sortedIDs(ids))
	t.Logf("cost: %s wei of balance for %d transactions", new(big.Int).Sub(balBefore, balAfter), concurrentLocks)
}

func sortedIDs(m map[types.EscrowID]int) []types.EscrowID {
	out := make([]types.EscrowID, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// diagnose turns "lock tx reverted" into something actionable: the nonce it
// used, whether it ran out of gas, and the reason the contract gave when the
// call is replayed against the block it landed in.
func diagnose(t *testing.T, ec *chain.EscrowChain, hash types.TxHash) string {
	t.Helper()
	if hash == "" {
		return "(never sent — no transaction hash)"
	}

	ctx := context.Background()
	client := ec.Client()
	h := common.HexToHash(string(hash))

	tx, _, err := client.TransactionByHash(ctx, h)
	if err != nil {
		return fmt.Sprintf("(tx %s not on chain: %v)", hash, err)
	}
	receipt, err := client.TransactionReceipt(ctx, h)
	if err != nil {
		return fmt.Sprintf("(tx %s has no receipt: %v)", hash, err)
	}

	// Monad charges — and reports — the gas limit rather than gas consumed, so
	// gasUsed always equals the limit here and can never indicate an
	// out-of-gas. The replay below is what actually distinguishes causes.

	// Replay the exact call against the state of the block before it ran, which
	// is what surfaces the revert reason.
	from, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(tx.ChainId()), tx)
	reason := ""
	if err == nil {
		_, callErr := client.CallContract(ctx, ethereum.CallMsg{
			From:     from,
			To:       tx.To(),
			Gas:      tx.Gas(),
			GasPrice: tx.GasPrice(),
			Value:    tx.Value(),
			Data:     tx.Data(),
		}, new(big.Int).Sub(receipt.BlockNumber, big.NewInt(1)))
		if callErr != nil {
			reason = " reason=" + callErr.Error()
		}
	}

	return fmt.Sprintf("[tx=%s nonce=%d block=%d gasLimit=%d%s]",
		hash, tx.Nonce(), receipt.BlockNumber, tx.Gas(), reason)
}
