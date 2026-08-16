//go:build integration

// Real-chain lifecycle failure paths. These lock actual MON on Monad testnet
// and assert the invariant that matters: when delivery is not verified, or
// when settlement itself fails, the money is still recoverable by the buyer.
//
// The sweeper can only refund after the contract's releaseDeadline, which
// comes from LEASH_DEFAULT_TTL — the default of 3600s would make these tests
// an hour long, so they skip unless it is set small.
//
// Run:
//
//	MONAD_RPC=https://testnet-rpc.monad.xyz \
//	BUYER_KEY=<hex> \
//	MERCHANT_KEY=<hex> \
//	LEASH_DEFAULT_TTL=60 \
//	go test -tags integration -timeout 900s -run Chain -v ./internal/engine/...
package engine

import (
	"context"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/A1kartikey/leash/internal/chain"
	"github.com/A1kartikey/leash/internal/chain/bindings"
	"github.com/A1kartikey/leash/internal/ledger"
	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// On-chain Status enum. A nonexistent escrow also reads as Locked, so every
// status assertion here is on an ID we locked ourselves.
const (
	onchainLocked   = 0
	onchainReleased = 1
	onchainRefunded = 2
)

// maxChainTTL bounds how long these tests are willing to wait for the
// releaseDeadline before giving up on the sweeper assertion.
const maxChainTTL = 300 * time.Second

// chainPrice is 0.0001 MON — enough to be real, small enough to burn.
func chainPrice() *big.Int { return big.NewInt(100_000_000_000_000) }

// chainEnv builds a real buyer-signed EscrowChain, or skips.
func chainEnv(t *testing.T) (*chain.EscrowChain, chain.Config, context.Context) {
	t.Helper()

	if os.Getenv("MONAD_RPC") == "" || os.Getenv("BUYER_KEY") == "" {
		t.Skip("MONAD_RPC and BUYER_KEY must be set for chain lifecycle tests")
	}

	cfg, err := chain.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if ttl := time.Duration(cfg.DefaultTTL) * time.Second; ttl > maxChainTTL {
		t.Skipf("LEASH_DEFAULT_TTL=%ds is too long for a refund test; set it to 60", cfg.DefaultTTL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	ec, err := chain.NewEscrowChain(ctx, cfg)
	if err != nil {
		cancel()
		t.Fatalf("NewEscrowChain: %v", err)
	}
	t.Cleanup(func() {
		ec.Close()
		cancel()
	})

	return ec, cfg, ctx
}

// chainEngine wires a real ledger and the real Verifier to c. The engine's TTL
// must match the contract's, or the sweeper refunds before the chain allows it.
func chainEngine(t *testing.T, c types.Chain, cfg chain.Config) (*Engine, *ledger.SQLiteLedger) {
	t.Helper()

	led, err := ledger.New(filepath.Join(t.TempDir(), "leash.db"))
	if err != nil {
		t.Fatalf("ledger.New: %v", err)
	}
	t.Cleanup(func() { led.Close() })

	econf := DefaultConfig()
	econf.TTL = time.Duration(cfg.DefaultTTL) * time.Second
	econf.SweepGrace = 15 * time.Second // block.timestamp drifts from our clock
	return New(SingleChain(c), led, Verifier{}, econf), led
}

// chainMerchant is the address escrows are locked to. Real if MERCHANT_KEY is
// set; otherwise a fixed non-zero address, which is fine because every test
// here ends in a refund, never a release.
func chainMerchant(t *testing.T) common.Address {
	t.Helper()
	if k := strings.TrimPrefix(os.Getenv("MERCHANT_KEY"), "0x"); k != "" {
		pk, err := crypto.HexToECDSA(k)
		if err != nil {
			t.Fatalf("parsing MERCHANT_KEY: %v", err)
		}
		return crypto.PubkeyToAddress(pk.PublicKey)
	}
	return common.HexToAddress("0x000000000000000000000000000000000000dEaD")
}

func onchainStatus(t *testing.T, ctx context.Context, c *bindings.PaymentEscrow, id types.EscrowID) uint8 {
	t.Helper()
	e, err := c.Escrows(&bind.CallOpts{Context: ctx}, new(big.Int).SetUint64(uint64(id)))
	if err != nil {
		t.Fatalf("reading escrow %d: %v", id, err)
	}
	return e.Status
}

// sweepUntilRefunded polls Sweep until the escrow is refunded on-chain. The
// sweeper is a no-op until the deadline plus grace has passed, so this is a
// wait loop, not a retry loop.
func sweepUntilRefunded(t *testing.T, ctx context.Context, e *Engine, ec *chain.EscrowChain, id types.EscrowID, ttl time.Duration) {
	t.Helper()

	deadline := time.Now().Add(ttl + e.cfg.SweepGrace + 3*time.Minute)
	for time.Now().Before(deadline) {
		n, err := e.Sweep(ctx, tenant)
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		if n > 0 {
			t.Logf("sweeper refunded %d escrow(s)", n)
			if got := onchainStatus(t, ctx, ec.Contract(), id); got != onchainRefunded {
				t.Fatalf("escrow %d: on-chain status %d, want Refunded(%d)", id, got, onchainRefunded)
			}
			return
		}
		time.Sleep(10 * time.Second)
	}
	t.Fatalf("escrow %d never refunded within the deadline", id)
}

func assertLedgerRefunded(t *testing.T, ctx context.Context, led *ledger.SQLiteLedger, amount *big.Int) {
	t.Helper()
	bal, err := led.Snapshot(ctx, tenant)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if bal.PendingCount != 0 {
		t.Fatalf("ledger still has %d pending obligations after refund", bal.PendingCount)
	}
	if bal.Recovered.Cmp(amount) != 0 {
		t.Fatalf("recovered %s wei, want %s", bal.Recovered, amount)
	}
}

// ---------------------------------------------------------------------------
// Absent delivery: the merchant takes the lock and delivers nothing usable.
// Nothing may be released; the sweeper must get the money back.
// ---------------------------------------------------------------------------

func TestChainAbsentDeliveryIsSweptToRefund(t *testing.T) {
	ec, cfg, ctx := chainEnv(t)
	e, led := chainEngine(t, ec, cfg)

	ch := types.Challenge{
		Price:        chainPrice(),
		Merchant:     chainMerchant(t),
		ResourceID:   "chain-absent",
		ResourceHash: hashOf(`{"temp":21}`),
	}
	// Paid, then a 500 with an empty body: the canonical absent delivery.
	srv := merchantServer(t, ch, 500, "application/json", "")

	res, err := fetch(t, e, srv.URL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !res.Paid || res.EscrowID == 0 {
		t.Fatalf("expected a real lock, got %+v", res)
	}
	t.Logf("locked escrow %d in %s", res.EscrowID, res.LockTx)

	if res.Verdict.Outcome != types.VerdictAbsent {
		t.Fatalf("verdict %s/%s, want absent", res.Verdict.Outcome, res.Verdict.Reason)
	}
	if res.SettleTx != "" {
		t.Fatalf("settled an absent delivery: tx %s", res.SettleTx)
	}
	// The invariant: no funds moved to the merchant.
	if got := onchainStatus(t, ctx, ec.Contract(), res.EscrowID); got != onchainLocked {
		t.Fatalf("escrow %d: on-chain status %d, want Locked(%d)", res.EscrowID, got, onchainLocked)
	}

	sweepUntilRefunded(t, ctx, e, ec, res.EscrowID, e.cfg.TTL)
	assertLedgerRefunded(t, ctx, led, ch.Price)
}

// ---------------------------------------------------------------------------
// Settlement failure: delivery verified, but the release transaction reverts.
// The obligation must stay LOCKED so the sweeper can still recover it.
// ---------------------------------------------------------------------------

// brokenRelease sends every release to an escrow ID that does not belong to
// the buyer, so the contract reverts with NotBuyer. That is a real on-chain
// revert and it leaves the real escrow untouched — exactly the state a
// settlement failure must be recoverable from.
type brokenRelease struct {
	types.Chain
	seen int
}

func (b *brokenRelease) Release(ctx context.Context, id types.EscrowID) (types.TxHash, error) {
	b.seen++
	return b.Chain.Release(ctx, id+1<<40)
}

func TestChainReleaseRevertLeavesEscrowRecoverable(t *testing.T) {
	ec, cfg, ctx := chainEnv(t)
	broken := &brokenRelease{Chain: ec}
	e, led := chainEngine(t, broken, cfg)

	const body = `{"temp":21}`
	ch := types.Challenge{
		Price:        chainPrice(),
		Merchant:     chainMerchant(t),
		ResourceID:   "chain-release-revert",
		ResourceHash: hashOf(body),
	}
	// The merchant delivers exactly what was paid for: verdict is Delivered,
	// so settle() attempts a release — and that is what fails.
	srv := merchantServer(t, ch, 200, "application/json", body)

	res, err := fetch(t, e, srv.URL)
	if err == nil {
		t.Fatal("expected a release error from Fetch")
	}
	if !strings.Contains(err.Error(), "release") {
		t.Fatalf("error %v, want a release failure", err)
	}
	if res == nil || res.EscrowID == 0 {
		t.Fatalf("expected the escrow id back with the error, got %+v", res)
	}
	if res.Verdict.Outcome != types.VerdictDelivered {
		t.Fatalf("verdict %s/%s, want delivered", res.Verdict.Outcome, res.Verdict.Reason)
	}
	if broken.seen != 1 {
		t.Fatalf("release attempted %d times, want 1", broken.seen)
	}
	if res.SettleTx != "" {
		t.Fatalf("recorded a settle tx for a reverted release: %s", res.SettleTx)
	}
	t.Logf("escrow %d: release reverted as intended", res.EscrowID)

	// Still locked on-chain, and still LOCKED in the ledger: a failed
	// settlement must never be recorded as settled.
	if got := onchainStatus(t, ctx, ec.Contract(), res.EscrowID); got != onchainLocked {
		t.Fatalf("escrow %d: on-chain status %d, want Locked(%d)", res.EscrowID, got, onchainLocked)
	}
	bal, err := led.Snapshot(ctx, tenant)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if bal.PendingCount != 1 || bal.Locked.Cmp(ch.Price) != 0 {
		t.Fatalf("ledger shows pending=%d locked=%s, want 1 / %s", bal.PendingCount, bal.Locked, ch.Price)
	}

	// The sweeper refunds through the real chain, not the broken shim: Refund
	// was never wrapped, which is the recovery path that has to keep working.
	sweepUntilRefunded(t, ctx, e, ec, res.EscrowID, e.cfg.TTL)
	assertLedgerRefunded(t, ctx, led, ch.Price)
}

// ---------------------------------------------------------------------------
// Circuit breaker: after the threshold is reached, the next call must not
// reach the chain at all. A 503 that still submitted a lock transaction is a
// worse bug than no 503 — so the assertion is on the contract's balance, not
// on the error.
// ---------------------------------------------------------------------------

// contractBalance is the total MON the escrow singleton holds. A lock adds the
// escrow price to it; nothing else this test does moves it.
func contractBalance(t *testing.T, ctx context.Context, ec *chain.EscrowChain, contract common.Address) *big.Int {
	t.Helper()
	b, err := ec.BalanceOf(ctx, contract)
	if err != nil {
		t.Fatalf("reading contract balance: %v", err)
	}
	return b
}

// stableContractBalance reads until two consecutive reads agree. Monad executes
// asynchronously, so a balance read taken right after a lock receipt can still
// be answered from a slightly older executed state — and that lagging value
// arriving between the before and after reads would look exactly like a
// transaction the fourth call made.
func stableContractBalance(t *testing.T, ctx context.Context, ec *chain.EscrowChain, contract common.Address) *big.Int {
	t.Helper()

	prev := contractBalance(t, ctx, ec, contract)
	for i := 0; i < 10; i++ {
		time.Sleep(2 * time.Second)
		cur := contractBalance(t, ctx, ec, contract)
		if cur.Cmp(prev) == 0 {
			return cur
		}
		t.Logf("contract balance still settling: %s -> %s", prev, cur)
		prev = cur
	}
	t.Log("contract balance never settled; a stranger is using the singleton concurrently")
	return prev
}

func TestChainCircuitOpensBeforeAnyLock(t *testing.T) {
	ec, cfg, ctx := chainEnv(t)
	e, led := chainEngine(t, ec, cfg)

	threshold := e.cfg.BreakerThreshold
	if threshold < 1 {
		t.Fatalf("breaker threshold %d makes this test meaningless", threshold)
	}

	ch := types.Challenge{
		Price:        chainPrice(),
		Merchant:     chainMerchant(t),
		ResourceID:   "chain-breaker",
		ResourceHash: hashOf(`{"temp":21}`),
	}
	// Every paid retry is a non-delivery, so every one of them is a real lock
	// followed by a real Absent verdict.
	srv := merchantServer(t, ch, 500, "application/json", "")

	// Baseline, so the reading below is known to be able to see a lock at all.
	baseline := stableContractBalance(t, ctx, ec, cfg.ContractAddr)

	var ids []types.EscrowID
	for i := 0; i < threshold; i++ {
		res, err := fetch(t, e, srv.URL)
		if err != nil {
			t.Fatalf("failure %d/%d: %v", i+1, threshold, err)
		}
		if res.EscrowID == 0 || res.LockTx == "" {
			t.Fatalf("failure %d/%d did not lock on-chain: %+v", i+1, threshold, res)
		}
		if res.Verdict.Outcome != types.VerdictAbsent {
			t.Fatalf("failure %d/%d: verdict %s/%s, want absent", i+1, threshold, res.Verdict.Outcome, res.Verdict.Reason)
		}
		if got := onchainStatus(t, ctx, ec.Contract(), res.EscrowID); got != onchainLocked {
			t.Fatalf("escrow %d: on-chain status %d, want Locked(%d)", res.EscrowID, got, onchainLocked)
		}
		ids = append(ids, res.EscrowID)
		t.Logf("failure %d/%d: escrow %d locked in %s, verdict absent", i+1, threshold, res.EscrowID, res.LockTx)
	}

	// The reading the whole test turns on, taken as late as possible before
	// the call that must not move it.
	before := stableContractBalance(t, ctx, ec, cfg.ContractAddr)
	t.Logf("contract holds %s wei with the circuit about to open (baseline %s, %d locks of %s)",
		before, baseline, threshold, ch.Price)

	// A balance that did not move across three real locks cannot detect a
	// fourth one either, which would make the assertion below vacuous. The
	// singleton is public, so this is a floor, not an equality.
	if before.Cmp(baseline) <= 0 {
		t.Fatalf("contract balance %s did not rise above the baseline %s across %d locks: "+
			"this reading cannot see a lock, so it cannot prove one did not happen",
			before, baseline, threshold)
	}

	res, err := fetch(t, e, srv.URL)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("call %d: err %v, want ErrCircuitOpen (the proxy maps it to 503)", threshold+1, err)
	}
	if res != nil {
		t.Fatalf("call %d returned a result alongside the open circuit: %+v", threshold+1, res)
	}

	// The assertion: no transaction was submitted. A 503 that still locked
	// funds is the failure this test exists to catch.
	after := contractBalance(t, ctx, ec, cfg.ContractAddr)
	if after.Cmp(before) != 0 {
		t.Fatalf("contract balance moved across the blocked call: %s -> %s (delta %s wei, escrow price %s). "+
			"Either the open circuit still submitted a lock, or another user of the public singleton locked concurrently",
			before, after, new(big.Int).Sub(after, before), ch.Price)
	}

	// Nothing was recorded locally either: the ledger still holds exactly the
	// obligations the real failures opened.
	bal, err := led.Snapshot(ctx, tenant)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	want := new(big.Int).Mul(ch.Price, big.NewInt(int64(threshold)))
	if bal.PendingCount != threshold || bal.Locked.Cmp(want) != 0 {
		t.Fatalf("ledger shows pending=%d locked=%s, want %d / %s", bal.PendingCount, bal.Locked, threshold, want)
	}

	// Leave no real funds stranded: the escrows the failures opened are still
	// the sweeper's to recover.
	sweepUntilRefunded(t, ctx, e, ec, ids[len(ids)-1], e.cfg.TTL)
	assertLedgerRefunded(t, ctx, led, want)
}
