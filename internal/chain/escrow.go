package chain

import (
	"context"
	"fmt"
	"math/big"

	"github.com/A1kartikey/leash/internal/chain/bindings"
	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// ---------------------------------------------------------------------------
// Confirmation policy: OPTIMISTIC WITH LOG RECONCILIATION
//
// A lock is treated as final the moment the tx receipt comes back with
// status == 1 (success). We do NOT wait for N additional confirmations.
//
// Rationale: Monad testnet has fast finality (~1s block time). Waiting for
// extra confirmations adds latency for marginal safety gain. Instead, on
// startup we reconcile by replaying FilterEscrowLocked from the last
// checkpoint block to `latest`. Any events missed during downtime are
// re-emitted into the Subscribe channel. This ensures consistency without
// per-tx confirmation delay.
//
// If Monad moves to a model where reorgs are possible (unlikely for a
// testnet singleton), switch to N=3 confirmations by polling receipt.
// BlockNumber + 3 <= head before returning from Lock.
// ---------------------------------------------------------------------------

// receiptLike abstracts tx receipt access for testability.
type receiptLike interface {
	GetLogs() []ethtypes.Log
}

// ethReceipt adapts *ethtypes.Receipt to receiptLike.
type ethReceipt struct{ r *ethtypes.Receipt }

func (e ethReceipt) GetLogs() []ethtypes.Log {
	out := make([]ethtypes.Log, len(e.r.Logs))
	for i, l := range e.r.Logs {
		out[i] = *l
	}
	return out
}

// EscrowChain implements types.Chain by wrapping the PaymentEscrow contract.
// The contract address comes from deployments.json — never hardcoded.
// Keys come from config only — never a literal, never committed.
type EscrowChain struct {
	client   *ethclient.Client
	contract *bindings.PaymentEscrow
	cfg      Config
	txReqCh  chan txRequest
	cancel   context.CancelFunc
}

// Compile-time check: EscrowChain implements types.Chain.
var _ types.Chain = (*EscrowChain)(nil)

// NewEscrowChain connects to the RPC endpoint, binds the contract, and
// starts the nonce-serialising signer goroutine.
func NewEscrowChain(ctx context.Context, cfg Config) (*EscrowChain, error) {
	client, err := ethclient.DialContext(ctx, cfg.RPC)
	if err != nil {
		return nil, fmt.Errorf("dialing %s: %w", cfg.RPC, err)
	}

	contract, err := bindings.NewPaymentEscrow(cfg.ContractAddr, client)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("binding contract at %s: %w", cfg.ContractAddr, err)
	}

	loopCtx, cancel := context.WithCancel(ctx)
	txReqCh := make(chan txRequest, 16)

	go signerLoop(loopCtx, client, cfg.PrivateKey, cfg.ChainID, txReqCh)

	return &EscrowChain{
		client:   client,
		contract: contract,
		cfg:      cfg,
		txReqCh:  txReqCh,
		cancel:   cancel,
	}, nil
}

// Close shuts down the signer goroutine and closes the RPC client.
func (ec *EscrowChain) Close() {
	ec.cancel()
	ec.client.Close()
}

// ---------------------------------------------------------------------------
// types.Chain implementation
// ---------------------------------------------------------------------------

// Lock creates a new escrow, sending `amount` wei to the contract.
// The escrow ID is discovered from the EscrowLocked event in the receipt —
// never inferred from nextId.
func (ec *EscrowChain) Lock(ctx context.Context, merchant common.Address, amount *big.Int, resourceHash [32]byte) (types.EscrowID, types.TxHash, error) {
	tx, err := submitTx(ctx, ec.txReqCh, func(opts *bind.TransactOpts) (*ethtypes.Transaction, error) {
		opts.Value = amount
		return ec.contract.Lock(opts, merchant, resourceHash, ec.cfg.DefaultTTL)
	})
	if err != nil {
		return 0, "", fmt.Errorf("lock tx: %w", err)
	}

	receipt, err := bind.WaitMined(ctx, ec.client, tx)
	if err != nil {
		return 0, types.TxHash(tx.Hash().Hex()), fmt.Errorf("waiting for lock receipt: %w", err)
	}
	if receipt.Status != ethtypes.ReceiptStatusSuccessful {
		return 0, types.TxHash(tx.Hash().Hex()), fmt.Errorf("lock tx reverted")
	}

	id, err := escrowIDFromLockReceipt(ec.contract, ethReceipt{r: receipt})
	if err != nil {
		return 0, types.TxHash(tx.Hash().Hex()), fmt.Errorf("parsing lock receipt: %w", err)
	}

	return types.EscrowID(id.Uint64()), types.TxHash(tx.Hash().Hex()), nil
}

// Release settles the full escrow amount to the merchant.
func (ec *EscrowChain) Release(ctx context.Context, id types.EscrowID) (types.TxHash, error) {
	tx, err := submitTx(ctx, ec.txReqCh, func(opts *bind.TransactOpts) (*ethtypes.Transaction, error) {
		return ec.contract.Release(opts, new(big.Int).SetUint64(uint64(id)))
	})
	if err != nil {
		return "", fmt.Errorf("release tx: %w", err)
	}

	receipt, err := bind.WaitMined(ctx, ec.client, tx)
	if err != nil {
		return types.TxHash(tx.Hash().Hex()), fmt.Errorf("waiting for release receipt: %w", err)
	}
	if receipt.Status != ethtypes.ReceiptStatusSuccessful {
		return types.TxHash(tx.Hash().Hex()), fmt.Errorf("release tx reverted")
	}

	return types.TxHash(tx.Hash().Hex()), nil
}

// Refund returns the full escrow amount to the buyer. Only callable after
// the releaseDeadline has passed.
func (ec *EscrowChain) Refund(ctx context.Context, id types.EscrowID) (types.TxHash, error) {
	tx, err := submitTx(ctx, ec.txReqCh, func(opts *bind.TransactOpts) (*ethtypes.Transaction, error) {
		return ec.contract.Refund(opts, new(big.Int).SetUint64(uint64(id)))
	})
	if err != nil {
		return "", fmt.Errorf("refund tx: %w", err)
	}

	receipt, err := bind.WaitMined(ctx, ec.client, tx)
	if err != nil {
		return types.TxHash(tx.Hash().Hex()), fmt.Errorf("waiting for refund receipt: %w", err)
	}
	if receipt.Status != ethtypes.ReceiptStatusSuccessful {
		return types.TxHash(tx.Hash().Hex()), fmt.Errorf("refund tx reverted")
	}

	return types.TxHash(tx.Hash().Hex()), nil
}

// ReleasePartial settles `amount` wei to the merchant and refunds the
// remainder to the buyer. Terminal either way.
func (ec *EscrowChain) ReleasePartial(ctx context.Context, id types.EscrowID, amount *big.Int) (types.TxHash, error) {
	tx, err := submitTx(ctx, ec.txReqCh, func(opts *bind.TransactOpts) (*ethtypes.Transaction, error) {
		return ec.contract.ReleasePartial(opts, new(big.Int).SetUint64(uint64(id)), amount)
	})
	if err != nil {
		return "", fmt.Errorf("releasePartial tx: %w", err)
	}

	receipt, err := bind.WaitMined(ctx, ec.client, tx)
	if err != nil {
		return types.TxHash(tx.Hash().Hex()), fmt.Errorf("waiting for releasePartial receipt: %w", err)
	}
	if receipt.Status != ethtypes.ReceiptStatusSuccessful {
		return types.TxHash(tx.Hash().Hex()), fmt.Errorf("releasePartial tx reverted")
	}

	return types.TxHash(tx.Hash().Hex()), nil
}

// Subscribe streams escrow events for the buyer's address.
// Uses poll-based log filtering (see subscribe.go).
// The returned channel is closed when ctx is cancelled.
func (ec *EscrowChain) Subscribe(ctx context.Context) (<-chan types.EscrowEvent, error) {
	head, err := ec.client.BlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting head block: %w", err)
	}

	out := make(chan types.EscrowEvent, 64)
	go eventPoller(ctx, ec.client, ec.contract, ec.cfg.SignerAddr, head, out)
	return out, nil
}

// BalanceOf returns the native MON balance of the given address.
func (ec *EscrowChain) BalanceOf(ctx context.Context, addr common.Address) (*big.Int, error) {
	bal, err := ec.client.BalanceAt(ctx, addr, nil)
	if err != nil {
		return nil, fmt.Errorf("balance query: %w", err)
	}
	return bal, nil
}

// ---------------------------------------------------------------------------
// Extra methods not in types.Chain — used by integration tests and batch ops
// ---------------------------------------------------------------------------

// LockBatch creates multiple escrows in a single transaction.
// Returns the escrow IDs discovered from the EscrowLocked events in the
// receipt.
func (ec *EscrowChain) LockBatch(ctx context.Context, merchant common.Address, hashes [][32]byte, amounts []*big.Int, ttl uint64) ([]types.EscrowID, types.TxHash, error) {
	total := new(big.Int)
	for _, a := range amounts {
		total.Add(total, a)
	}

	tx, err := submitTx(ctx, ec.txReqCh, func(opts *bind.TransactOpts) (*ethtypes.Transaction, error) {
		opts.Value = total
		return ec.contract.LockBatch(opts, merchant, hashes, amounts, ttl)
	})
	if err != nil {
		return nil, "", fmt.Errorf("lockBatch tx: %w", err)
	}

	receipt, err := bind.WaitMined(ctx, ec.client, tx)
	if err != nil {
		return nil, types.TxHash(tx.Hash().Hex()), fmt.Errorf("waiting for lockBatch receipt: %w", err)
	}
	if receipt.Status != ethtypes.ReceiptStatusSuccessful {
		return nil, types.TxHash(tx.Hash().Hex()), fmt.Errorf("lockBatch tx reverted")
	}

	bigIDs, err := escrowIDsFromBatchReceipt(ec.contract, ethReceipt{r: receipt})
	if err != nil {
		return nil, types.TxHash(tx.Hash().Hex()), fmt.Errorf("parsing lockBatch receipt: %w", err)
	}

	ids := make([]types.EscrowID, len(bigIDs))
	for i, b := range bigIDs {
		ids[i] = types.EscrowID(b.Uint64())
	}

	return ids, types.TxHash(tx.Hash().Hex()), nil
}

// ReleaseMany releases multiple escrows in a single transaction.
func (ec *EscrowChain) ReleaseMany(ctx context.Context, ids []types.EscrowID) (types.TxHash, error) {
	bigIDs := make([]*big.Int, len(ids))
	for i, id := range ids {
		bigIDs[i] = new(big.Int).SetUint64(uint64(id))
	}

	tx, err := submitTx(ctx, ec.txReqCh, func(opts *bind.TransactOpts) (*ethtypes.Transaction, error) {
		return ec.contract.ReleaseMany(opts, bigIDs)
	})
	if err != nil {
		return "", fmt.Errorf("releaseMany tx: %w", err)
	}

	receipt, err := bind.WaitMined(ctx, ec.client, tx)
	if err != nil {
		return types.TxHash(tx.Hash().Hex()), fmt.Errorf("waiting for releaseMany receipt: %w", err)
	}
	if receipt.Status != ethtypes.ReceiptStatusSuccessful {
		return types.TxHash(tx.Hash().Hex()), fmt.Errorf("releaseMany tx reverted")
	}

	return types.TxHash(tx.Hash().Hex()), nil
}

// Claim is the merchant's backstop — callable only after claimDeadline.
// This is NOT part of types.Chain (buyer's interface) but is needed for
// the integration test that exercises the claim path.
func (ec *EscrowChain) Claim(ctx context.Context, id types.EscrowID) (types.TxHash, error) {
	tx, err := submitTx(ctx, ec.txReqCh, func(opts *bind.TransactOpts) (*ethtypes.Transaction, error) {
		return ec.contract.Claim(opts, new(big.Int).SetUint64(uint64(id)))
	})
	if err != nil {
		return "", fmt.Errorf("claim tx: %w", err)
	}

	receipt, err := bind.WaitMined(ctx, ec.client, tx)
	if err != nil {
		return types.TxHash(tx.Hash().Hex()), fmt.Errorf("waiting for claim receipt: %w", err)
	}
	if receipt.Status != ethtypes.ReceiptStatusSuccessful {
		return types.TxHash(tx.Hash().Hex()), fmt.Errorf("claim tx reverted")
	}

	return types.TxHash(tx.Hash().Hex()), nil
}

// Contract exposes the raw binding for advanced queries (e.g. Escrows() read).
func (ec *EscrowChain) Contract() *bindings.PaymentEscrow {
	return ec.contract
}

// Client exposes the ethclient for direct RPC calls in tests.
func (ec *EscrowChain) Client() *ethclient.Client {
	return ec.client
}
