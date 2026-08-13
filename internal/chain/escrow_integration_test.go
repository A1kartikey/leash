//go:build integration

// Package chain_test contains integration tests that exercise all five escrow
// paths against the live Monad testnet. They require funded keys and a
// deployed contract.
//
// Run:
//
//	MONAD_RPC=https://testnet-rpc.monad.xyz \
//	BUYER_KEY=<hex> \
//	MERCHANT_KEY=<hex> \
//	go test -v -tags integration -timeout 600s ./internal/chain/...
package chain_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/A1kartikey/leash/internal/chain"
	"github.com/A1kartikey/leash/internal/chain/bindings"
	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// testEnv loads the environment and builds a buyer EscrowChain.
// Skips the test if env vars are missing.
func testEnv(t *testing.T) (*chain.EscrowChain, context.Context, context.CancelFunc) {
	t.Helper()

	rpc := os.Getenv("MONAD_RPC")
	buyerKey := os.Getenv("BUYER_KEY")
	if rpc == "" || buyerKey == "" {
		t.Skip("MONAD_RPC and BUYER_KEY must be set for integration tests")
	}

	// Ensure deployments.json is valid.
	_ = os.Getenv("LEASH_DEPLOYMENTS") // optional override

	cfg, err := chain.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)

	ec, err := chain.NewEscrowChain(ctx, cfg)
	if err != nil {
		cancel()
		t.Fatalf("NewEscrowChain: %v", err)
	}

	t.Cleanup(func() {
		ec.Close()
		cancel()
	})

	return ec, ctx, cancel
}

// merchantChain creates a second EscrowChain using MERCHANT_KEY so the claim
// test can call claim() as the merchant.
func merchantChain(t *testing.T, ctx context.Context) *chain.EscrowChain {
	t.Helper()

	merchantKeyHex := os.Getenv("MERCHANT_KEY")
	if merchantKeyHex == "" {
		t.Skip("MERCHANT_KEY must be set for claim test")
	}
	merchantKeyHex = strings.TrimPrefix(merchantKeyHex, "0x")

	pk, err := crypto.HexToECDSA(merchantKeyHex)
	if err != nil {
		t.Fatalf("parsing MERCHANT_KEY: %v", err)
	}

	cfg, err := chain.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig for merchant: %v", err)
	}

	// Override the key to the merchant's key.
	cfg.PrivateKey = pk
	cfg.SignerAddr = crypto.PubkeyToAddress(pk.PublicKey)

	ec, err := chain.NewEscrowChain(ctx, cfg)
	if err != nil {
		t.Fatalf("NewEscrowChain (merchant): %v", err)
	}

	t.Cleanup(func() { ec.Close() })
	return ec
}

// randomHash generates a random 32-byte resource hash for testing.
func randomHash() [32]byte {
	var h [32]byte
	rand.Read(h[:])
	return h
}

// smallAmount returns a small wei amount for testing (0.0001 MON = 10^14 wei).
func smallAmount() *big.Int {
	return new(big.Int).Mul(big.NewInt(100_000_000_000_000), big.NewInt(1)) // 0.0001 MON
}

// merchantAddr returns the merchant address from the MERCHANT_KEY env var.
func merchantAddr(t *testing.T) common.Address {
	t.Helper()
	keyHex := os.Getenv("MERCHANT_KEY")
	if keyHex == "" {
		t.Skip("MERCHANT_KEY required")
	}
	keyHex = strings.TrimPrefix(keyHex, "0x")
	pk, err := crypto.HexToECDSA(keyHex)
	if err != nil {
		t.Fatalf("parsing MERCHANT_KEY: %v", err)
	}
	return crypto.PubkeyToAddress(pk.PublicKey)
}

// waitForBlock waits until the chain head reaches at least `target`.
func waitForBlock(ctx context.Context, client *ethclient.Client, target uint64) error {
	for {
		head, err := client.BlockNumber(ctx)
		if err != nil {
			return err
		}
		if head >= target {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
}

// escrowOnChain reads the on-chain escrow struct by ID.
func escrowOnChain(t *testing.T, ctx context.Context, contract *bindings.PaymentEscrow, id types.EscrowID) struct {
	Buyer           common.Address
	Merchant        common.Address
	Amount          *big.Int
	ResourceHash    [32]byte
	ReleaseDeadline uint64
	ClaimDeadline   uint64
	Status          uint8
} {
	t.Helper()
	e, err := contract.Escrows(&bind.CallOpts{Context: ctx}, new(big.Int).SetUint64(uint64(id)))
	if err != nil {
		t.Fatalf("reading escrow %d: %v", id, err)
	}
	return e
}

// ---------------------------------------------------------------------------
// Test 1: Lock → Release
// ---------------------------------------------------------------------------

func TestLockRelease(t *testing.T) {
	ec, ctx, _ := testEnv(t)
	merchant := merchantAddr(t)
	amount := smallAmount()
	hash := randomHash()

	t.Log("locking escrow...")
	id, txHash, err := ec.Lock(ctx, merchant, amount, hash)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	t.Logf("locked: id=%d tx=%s", id, txHash)

	// Verify on-chain state.
	e := escrowOnChain(t, ctx, ec.Contract(), id)
	if e.Status != 0 { // 0 = Locked
		t.Fatalf("expected status Locked (0), got %d", e.Status)
	}
	if e.Amount.Cmp(amount) != 0 {
		t.Fatalf("expected amount %s, got %s", amount, e.Amount)
	}

	t.Log("releasing escrow...")
	releaseTx, err := ec.Release(ctx, id)
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	t.Logf("released: tx=%s", releaseTx)

	// Verify on-chain state post-release.
	e = escrowOnChain(t, ctx, ec.Contract(), id)
	if e.Status != 1 { // 1 = Released
		t.Fatalf("expected status Released (1), got %d", e.Status)
	}
}

// ---------------------------------------------------------------------------
// Test 2: Lock → Wait → Refund
// ---------------------------------------------------------------------------

func TestLockWaitRefund(t *testing.T) {
	ec, ctx, _ := testEnv(t)
	merchant := merchantAddr(t)
	amount := smallAmount()
	hash := randomHash()

	// Use a short TTL so releaseDeadline passes quickly.
	// The contract requires ttl > 0 and releaseDeadline < claimDeadline,
	// which means ttl must be at least 1. We use 60s.
	t.Log("locking escrow with short TTL for refund test...")
	id, txHash, err := ec.Lock(ctx, merchant, amount, hash)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	t.Logf("locked: id=%d tx=%s", id, txHash)

	// Read the releaseDeadline from on-chain.
	e := escrowOnChain(t, ctx, ec.Contract(), id)
	deadline := time.Unix(int64(e.ReleaseDeadline), 0)
	wait := time.Until(deadline) + 5*time.Second
	if wait > 0 {
		t.Logf("waiting %s for releaseDeadline to pass...", wait)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			t.Fatalf("context cancelled while waiting: %v", ctx.Err())
		}
	}

	t.Log("refunding escrow...")
	refundTx, err := ec.Refund(ctx, id)
	if err != nil {
		t.Fatalf("Refund: %v", err)
	}
	t.Logf("refunded: tx=%s", refundTx)

	e = escrowOnChain(t, ctx, ec.Contract(), id)
	if e.Status != 2 { // 2 = Refunded
		t.Fatalf("expected status Refunded (2), got %d", e.Status)
	}
}

// ---------------------------------------------------------------------------
// Test 3: Lock → ReleasePartial
// ---------------------------------------------------------------------------

func TestLockReleasePartial(t *testing.T) {
	ec, ctx, _ := testEnv(t)
	merchant := merchantAddr(t)
	total := new(big.Int).Mul(smallAmount(), big.NewInt(2)) // 0.0002 MON
	half := smallAmount()                                   // 0.0001 MON
	hash := randomHash()

	t.Log("locking escrow for partial release...")
	id, txHash, err := ec.Lock(ctx, merchant, total, hash)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	t.Logf("locked: id=%d tx=%s", id, txHash)

	t.Log("releasing partial amount...")
	partialTx, err := ec.ReleasePartial(ctx, id, half)
	if err != nil {
		t.Fatalf("ReleasePartial: %v", err)
	}
	t.Logf("partial release: tx=%s", partialTx)

	e := escrowOnChain(t, ctx, ec.Contract(), id)
	if e.Status != 3 { // 3 = Partial
		t.Fatalf("expected status Partial (3), got %d", e.Status)
	}
}

// ---------------------------------------------------------------------------
// Test 4: Lock → Wait → Claim (merchant calls claim after claimDeadline)
// ---------------------------------------------------------------------------

func TestLockWaitClaim(t *testing.T) {
	ec, ctx, _ := testEnv(t)
	mec := merchantChain(t, ctx)
	merchant := merchantAddr(t)
	amount := smallAmount()
	hash := randomHash()

	t.Log("locking escrow for claim test...")
	id, txHash, err := ec.Lock(ctx, merchant, amount, hash)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	t.Logf("locked: id=%d tx=%s", id, txHash)

	// Read the claimDeadline from on-chain.
	e := escrowOnChain(t, ctx, ec.Contract(), id)
	deadline := time.Unix(int64(e.ClaimDeadline), 0)
	wait := time.Until(deadline) + 5*time.Second
	if wait > 0 {
		t.Logf("waiting %s for claimDeadline to pass...", wait)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			t.Fatalf("context cancelled while waiting: %v", ctx.Err())
		}
	}

	t.Log("merchant claiming escrow...")
	claimTx, err := mec.Claim(ctx, id)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	t.Logf("claimed: tx=%s", claimTx)

	e = escrowOnChain(t, ctx, ec.Contract(), id)
	if e.Status != 1 { // 1 = Released (claim sets Released status)
		t.Fatalf("expected status Released (1) after claim, got %d", e.Status)
	}
}

// ---------------------------------------------------------------------------
// Test 5: LockBatch → ReleaseMany
// ---------------------------------------------------------------------------

func TestLockBatchReleaseMany(t *testing.T) {
	ec, ctx, _ := testEnv(t)
	merchant := merchantAddr(t)

	hashes := [][32]byte{randomHash(), randomHash(), randomHash()}
	amt := smallAmount()
	amounts := []*big.Int{
		new(big.Int).Set(amt),
		new(big.Int).Set(amt),
		new(big.Int).Set(amt),
	}
	ttl := uint64(3600)

	t.Log("locking batch of 3 escrows...")
	ids, batchTx, err := ec.LockBatch(ctx, merchant, hashes, amounts, ttl)
	if err != nil {
		t.Fatalf("LockBatch: %v", err)
	}
	t.Logf("batch locked: ids=%v tx=%s", ids, batchTx)

	if len(ids) != 3 {
		t.Fatalf("expected 3 IDs, got %d", len(ids))
	}

	// Verify all are Locked on-chain.
	for _, id := range ids {
		e := escrowOnChain(t, ctx, ec.Contract(), id)
		if e.Status != 0 {
			t.Fatalf("escrow %d: expected status Locked (0), got %d", id, e.Status)
		}
	}

	t.Log("releasing all 3 escrows via releaseMany...")
	releaseTx, err := ec.ReleaseMany(ctx, ids)
	if err != nil {
		t.Fatalf("ReleaseMany: %v", err)
	}
	t.Logf("releaseMany: tx=%s", releaseTx)

	// Verify all are Released on-chain.
	for _, id := range ids {
		e := escrowOnChain(t, ctx, ec.Contract(), id)
		if e.Status != 1 {
			t.Fatalf("escrow %d: expected status Released (1), got %d", id, e.Status)
		}
	}
}

// Suppress unused import warnings for packages needed by the test helpers.
var (
	_ *ecdsa.PrivateKey
	_ ethtypes.Transaction
)
