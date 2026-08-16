package tenant_test

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/A1kartikey/leash/internal/engine"
	"github.com/A1kartikey/leash/internal/ledger"
	"github.com/A1kartikey/leash/internal/tenant"
	"github.com/A1kartikey/leash/internal/types"
	"github.com/A1kartikey/leash/internal/types/mocks"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	tenantA = types.TenantID("agent-a")
	tenantB = types.TenantID("agent-b")

	// Escrow ids come from disjoint ranges, so a row that crossed tenants is
	// identifiable on sight rather than by counting.
	baseA = 1_000
	baseB = 2_000

	// Enough traffic that the two tenants genuinely overlap: B writes two rows
	// per purchase (lock, then settle) while A is still hammering the merchant.
	runsA = 30
	runsB = 30

	// A trips its circuit on the third consecutive non-delivery, so it locks
	// three escrows and is then refused before any further funds move.
	threshold = 3
	locksA    = threshold
)

var (
	price = big.NewInt(1_000_000_000_000_000) // 0.001 MON

	// One merchant, both tenants. Breaker state is keyed on (tenant, merchant),
	// so this is the case that a shared counter would break.
	sharedMerchant = common.HexToAddress("0x000000000000000000000000000000000000dEaD")

	deliveredBody = `{"temp":21}`
)

// tenantChain is one tenant's signer: its own handle, its own id range, and
// tx hashes tagged with the tenant so a settlement signed by the wrong handle
// is obvious in a failure message.
func tenantChain(name string, base uint64) *mocks.MockChain {
	var n atomic.Uint64
	c := &mocks.MockChain{}
	c.LockFn = func(context.Context, common.Address, *big.Int, [32]byte) (types.EscrowID, types.TxHash, error) {
		id := base + n.Add(1)
		return types.EscrowID(id), types.TxHash(fmt.Sprintf("0x%s-lock-%d", name, id)), nil
	}
	c.ReleaseFn = func(_ context.Context, id types.EscrowID) (types.TxHash, error) {
		return types.TxHash(fmt.Sprintf("0x%s-release-%d", name, id)), nil
	}
	c.RefundFn = func(_ context.Context, id types.EscrowID) (types.TxHash, error) {
		return types.TxHash(fmt.Sprintf("0x%s-refund-%d", name, id)), nil
	}
	return c
}

// merchantServer answers first contact with the challenge and then serves
// status/body on the paid retry. Both servers advertise the same merchant.
func merchantServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	ch := types.Challenge{
		Price:        price,
		Merchant:     sharedMerchant,
		ResourceID:   "weather/nyc",
		ResourceHash: crypto.Keccak256Hash([]byte(deliveredBody)),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(engine.HdrEscrowID) == "" {
			engine.WriteChallenge(w.Header(), ch)
			w.WriteHeader(http.StatusPaymentRequired)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fetch(ctx context.Context, e *engine.Engine, id types.TenantID, url string) (*engine.Result, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return e.Fetch(ctx, id, req)
}

// inRange reports whether an escrow id belongs to the given tenant's range.
func inRange(id types.EscrowID, base uint64) bool {
	return uint64(id) > base && uint64(id) < base+1_000
}

// Two tenants, one Leash process, one ledger file, running at the same time.
// The invariant under test is that nothing crosses: not a ledger row, not a
// breaker counter, not a signer.
func TestTwoTenantsStayIsolatedUnderConcurrentLifecycles(t *testing.T) {
	led, err := ledger.New(filepath.Join(t.TempDir(), "obligations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer led.Close()

	chainA := tenantChain("a", baseA)
	chainB := tenantChain("b", baseB)

	reg := tenant.New()
	if err := reg.Add(tenantA, common.HexToAddress("0x00000000000000000000000000000000000000AA"), chainA); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(tenantB, common.HexToAddress("0x00000000000000000000000000000000000000BB"), chainB); err != nil {
		t.Fatal(err)
	}

	cfg := engine.DefaultConfig()
	cfg.TTL = 0 // every obligation is sweepable the moment it is written
	cfg.SweepGrace = 0
	cfg.BreakerThreshold = threshold
	e := engine.New(reg.ChainFor(), led, engine.Verifier{}, cfg)

	// A buys from a merchant that takes the lock and delivers nothing.
	// B buys the real resource from the same merchant address.
	failing := merchantServer(t, http.StatusInternalServerError, "")
	honest := merchantServer(t, http.StatusOK, deliveredBody)

	ctx := context.Background()

	// The watchdog runs for the whole test, not just at the end: "never
	// returns the other tenant's row" is a claim about every instant, and a
	// leak that healed before the final assertion would otherwise pass.
	var (
		stop     = make(chan struct{})
		watchdog sync.WaitGroup
		leakMu   sync.Mutex
		leaks    []string
		polls    atomic.Int64
	)
	check := func(id types.TenantID, base uint64, maxRows int) {
		pending, err := led.Pending(ctx, id)
		if err != nil {
			return // a busy SQLite file is not the property under test
		}
		snap, err := led.Snapshot(ctx, id)
		if err != nil {
			return
		}
		polls.Add(1)

		leakMu.Lock()
		defer leakMu.Unlock()
		for _, ob := range pending {
			if ob.TenantID != id {
				leaks = append(leaks, fmt.Sprintf("Pending(%s) returned a row owned by %s (escrow %d)", id, ob.TenantID, ob.EscrowID))
			}
			if !inRange(ob.EscrowID, base) {
				leaks = append(leaks, fmt.Sprintf("Pending(%s) returned escrow %d, outside this tenant's id range", id, ob.EscrowID))
			}
		}
		if snap.PendingCount > maxRows {
			leaks = append(leaks, fmt.Sprintf("Snapshot(%s) counted %d pending, more than the %d this tenant can have", id, snap.PendingCount, maxRows))
		}
		if limit := new(big.Int).Mul(price, big.NewInt(int64(maxRows))); snap.Locked.Cmp(limit) > 0 {
			leaks = append(leaks, fmt.Sprintf("Snapshot(%s) reported %s wei locked, more than this tenant could lock (%s)", id, snap.Locked, limit))
		}
	}

	watchdog.Add(1)
	go func() {
		defer watchdog.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			check(tenantA, baseA, locksA)
			check(tenantB, baseB, runsB)
			// The ledger is one connection wide; yield so polling cannot
			// starve the lifecycles it is meant to be observing.
			time.Sleep(200 * time.Microsecond)
		}
	}()

	// --- both tenants run their full lifecycles at the same time -----------
	var wg sync.WaitGroup
	var circuitRefusals atomic.Int64

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < runsA; i++ {
			res, err := fetch(ctx, e, tenantA, failing.URL)
			switch {
			case errors.Is(err, engine.ErrCircuitOpen):
				circuitRefusals.Add(1)
			case err != nil:
				t.Errorf("tenant A fetch %d: %v", i, err)
			case res.Verdict.Outcome != types.VerdictAbsent:
				t.Errorf("tenant A fetch %d: verdict %s, want absent", i, res.Verdict.Outcome)
			case !inRange(res.EscrowID, baseA):
				t.Errorf("tenant A got escrow %d, which is not from its own chain", res.EscrowID)
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < runsB; i++ {
			res, err := fetch(ctx, e, tenantB, honest.URL)
			switch {
			case err != nil:
				t.Errorf("tenant B fetch %d: %v", i, err)
			case res.Verdict.Outcome != types.VerdictDelivered:
				t.Errorf("tenant B fetch %d: verdict %s/%s, want delivered", i, res.Verdict.Outcome, res.Verdict.Reason)
			case !inRange(res.EscrowID, baseB):
				t.Errorf("tenant B got escrow %d, which is not from its own chain", res.EscrowID)
			}
		}
	}()

	wg.Wait()
	close(stop)
	watchdog.Wait()

	if len(leaks) > 0 {
		t.Fatalf("cross-tenant leak observed over %d polls:\n  %s", polls.Load(), leaks[0])
	}
	t.Logf("%d concurrent ledger polls, no cross-tenant row", polls.Load())

	// --- ledger: each tenant sees only its own -----------------------------
	snapA, err := led.Snapshot(ctx, tenantA)
	if err != nil {
		t.Fatal(err)
	}
	snapB, err := led.Snapshot(ctx, tenantB)
	if err != nil {
		t.Fatal(err)
	}

	wantLockedA := new(big.Int).Mul(price, big.NewInt(locksA))
	if snapA.PendingCount != locksA || snapA.Locked.Cmp(wantLockedA) != 0 {
		t.Fatalf("tenant A: pending=%d locked=%s, want %d / %s", snapA.PendingCount, snapA.Locked, locksA, wantLockedA)
	}
	// B released everything it bought, so it holds nothing.
	if snapB.PendingCount != 0 || snapB.Locked.Sign() != 0 {
		t.Fatalf("tenant B: pending=%d locked=%s, want 0 / 0", snapB.PendingCount, snapB.Locked)
	}
	if circuitRefusals.Load() != runsA-locksA {
		t.Fatalf("tenant A was refused %d times, want %d", circuitRefusals.Load(), runsA-locksA)
	}

	// --- breaker: same merchant, opposite verdicts, independent state ------
	if got := e.Breaker().State(tenantA, sharedMerchant); got != engine.BreakerOpen {
		t.Fatalf("tenant A circuit is %s, want open", got)
	}
	if got := e.Breaker().State(tenantB, sharedMerchant); got != engine.BreakerClosed {
		t.Fatalf("tenant B circuit is %s for the same merchant, want closed — breaker state crossed tenants", got)
	}

	// --- sweeper: refunds only the tenant it was called for -----------------
	n, err := e.Sweep(ctx, tenantA)
	if err != nil {
		t.Fatal(err)
	}
	if n != locksA {
		t.Fatalf("sweeping tenant A refunded %d, want %d", n, locksA)
	}
	if n, err := e.Sweep(ctx, tenantB); err != nil || n != 0 {
		t.Fatalf("sweeping tenant B refunded %d (err %v), want 0 — it has nothing outstanding", n, err)
	}

	// --- signers: every call landed on the right handle ---------------------
	assertChainSawOnly(t, "A", chainA, baseA)
	assertChainSawOnly(t, "B", chainB, baseB)

	if locks := countCalls(chainA, "Lock"); locks != locksA {
		t.Fatalf("tenant A's signer made %d locks, want %d", locks, locksA)
	}
	if locks := countCalls(chainB, "Lock"); locks != runsB {
		t.Fatalf("tenant B's signer made %d locks, want %d", locks, runsB)
	}
	if refunds := countCalls(chainB, "Refund"); refunds != 0 {
		t.Fatalf("tenant B's signer was asked for %d refunds by another tenant's sweep", refunds)
	}

	// --- no signer without a tenant ----------------------------------------
	if _, err := reg.Chain("agent-c"); err == nil {
		t.Fatal("registry handed out a chain for an unregistered tenant")
	}
	if err := reg.Add("agent-c", common.Address{}, chainA); err == nil {
		t.Fatal("registry let a second tenant share tenant A's signer")
	}
}

// assertChainSawOnly checks that every escrow id this signer was asked to act
// on came from its own tenant's range.
func assertChainSawOnly(t *testing.T, name string, c *mocks.MockChain, base uint64) {
	t.Helper()
	for _, call := range c.Calls {
		if len(call.Args) == 0 {
			continue
		}
		id, ok := call.Args[0].(types.EscrowID)
		if !ok {
			continue // Lock passes a merchant address first
		}
		if !inRange(id, base) {
			t.Fatalf("tenant %s's signer was asked to %s escrow %d, which belongs to another tenant", name, call.Method, id)
		}
	}
}

func countCalls(c *mocks.MockChain, method string) int {
	n := 0
	for _, call := range c.Calls {
		if call.Method == method {
			n++
		}
	}
	return n
}

// A tenant added while the process is running must be swept too, which is why
// the sweeper reads the registry every tick instead of once at startup.
func TestRegistryIDsPickUpRuntimeAdditions(t *testing.T) {
	reg := tenant.New()
	if got := reg.IDs(); len(got) != 0 {
		t.Fatalf("new registry lists %v", got)
	}
	if err := reg.Add(tenantB, common.Address{}, &mocks.MockChain{}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(tenantA, common.Address{}, &mocks.MockChain{}); err != nil {
		t.Fatal(err)
	}

	got := reg.IDs()
	if len(got) != 2 || got[0] != tenantA || got[1] != tenantB {
		t.Fatalf("IDs() = %v, want [%s %s] in stable order", got, tenantA, tenantB)
	}
}
