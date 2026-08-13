package ledger

import (
	"context"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum/common"
)

// testLedger creates a fresh SQLiteLedger in a temp directory, returning the
// ledger and a cleanup func.
func testLedger(t *testing.T) *SQLiteLedger {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	l, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func testObligation(id types.EscrowID, tenant types.TenantID, amount *big.Int) types.Obligation {
	return types.Obligation{
		EscrowID:        id,
		TenantID:        tenant,
		Merchant:        common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Amount:          amount,
		ResourceHash:    [32]byte{0xaa, 0xbb, 0xcc},
		LockedAt:        time.Now().Add(-10 * time.Minute),
		ReleaseDeadline: time.Now().Add(-1 * time.Minute), // already past
		Status:          types.StatusLocked,
	}
}

// ---------------------------------------------------------------------------
// Test: Idempotent Open (PK dedup)
// ---------------------------------------------------------------------------

func TestOpenIdempotent(t *testing.T) {
	l := testLedger(t)
	ctx := context.Background()
	tenant := types.TenantID("t1")
	ob := testObligation(42, tenant, big.NewInt(1_000_000))

	// First insert succeeds.
	if err := l.Open(ctx, tenant, ob); err != nil {
		t.Fatalf("first Open: %v", err)
	}

	// Duplicate insert is a no-op, not an error.
	if err := l.Open(ctx, tenant, ob); err != nil {
		t.Fatalf("duplicate Open should be no-op, got: %v", err)
	}

	// Snapshot should show exactly one obligation, not two.
	snap, err := l.Snapshot(ctx, tenant)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.PendingCount != 1 {
		t.Fatalf("expected PendingCount=1 after duplicate Open, got %d", snap.PendingCount)
	}
}

// ---------------------------------------------------------------------------
// Test: Wei stored as TEXT / *big.Int round-trip
// ---------------------------------------------------------------------------

func TestWeiRoundTrip(t *testing.T) {
	l := testLedger(t)
	ctx := context.Background()
	tenant := types.TenantID("t1")

	// Use a value larger than int64 max to verify TEXT storage.
	huge, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	ob := testObligation(1, tenant, huge)

	if err := l.Open(ctx, tenant, ob); err != nil {
		t.Fatalf("Open: %v", err)
	}

	snap, err := l.Snapshot(ctx, tenant)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.Locked.Cmp(huge) != 0 {
		t.Fatalf("Locked = %s, want %s", snap.Locked, huge)
	}
}

// ---------------------------------------------------------------------------
// Test: Tenant isolation
// ---------------------------------------------------------------------------

func TestTenantIsolation(t *testing.T) {
	l := testLedger(t)
	ctx := context.Background()

	t1 := types.TenantID("alice")
	t2 := types.TenantID("bob")

	if err := l.Open(ctx, t1, testObligation(1, t1, big.NewInt(100))); err != nil {
		t.Fatal(err)
	}
	if err := l.Open(ctx, t2, testObligation(2, t2, big.NewInt(200))); err != nil {
		t.Fatal(err)
	}

	s1, _ := l.Snapshot(ctx, t1)
	s2, _ := l.Snapshot(ctx, t2)

	if s1.PendingCount != 1 || s1.Locked.Int64() != 100 {
		t.Fatalf("alice: pendingCount=%d locked=%s, want 1 / 100", s1.PendingCount, s1.Locked)
	}
	if s2.PendingCount != 1 || s2.Locked.Int64() != 200 {
		t.Fatalf("bob: pendingCount=%d locked=%s, want 1 / 200", s2.PendingCount, s2.Locked)
	}
}

// ---------------------------------------------------------------------------
// Test: MarkDelivered
// ---------------------------------------------------------------------------

func TestMarkDelivered(t *testing.T) {
	l := testLedger(t)
	ctx := context.Background()
	tenant := types.TenantID("t1")
	ob := testObligation(10, tenant, big.NewInt(500))

	l.Open(ctx, tenant, ob)

	now := time.Now()
	if err := l.MarkDelivered(ctx, tenant, 10, now); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}

	// Cross-tenant should fail.
	err := l.MarkDelivered(ctx, types.TenantID("other"), 10, now)
	if err == nil {
		t.Fatal("MarkDelivered should fail for wrong tenant")
	}
}

// ---------------------------------------------------------------------------
// Test: MarkSettled
// ---------------------------------------------------------------------------

func TestMarkSettled(t *testing.T) {
	l := testLedger(t)
	ctx := context.Background()
	tenant := types.TenantID("t1")
	ob := testObligation(20, tenant, big.NewInt(1000))

	l.Open(ctx, tenant, ob)

	if err := l.MarkSettled(ctx, tenant, 20, types.StatusRefunded, "0xdeadbeef"); err != nil {
		t.Fatalf("MarkSettled: %v", err)
	}

	snap, _ := l.Snapshot(ctx, tenant)
	// After refund: locked=0, recovered=1000
	if snap.Locked.Sign() != 0 {
		t.Fatalf("Locked should be 0 after refund, got %s", snap.Locked)
	}
	if snap.Recovered.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("Recovered should be 1000 after refund, got %s", snap.Recovered)
	}
	if snap.PendingCount != 0 {
		t.Fatalf("PendingCount should be 0 after settle, got %d", snap.PendingCount)
	}
}

// ---------------------------------------------------------------------------
// Test: Pending returns only LOCKED past release_deadline, oldest first
// ---------------------------------------------------------------------------

func TestPending(t *testing.T) {
	l := testLedger(t)
	ctx := context.Background()
	tenant := types.TenantID("t1")

	// Obligation 1: deadline already passed.
	ob1 := testObligation(1, tenant, big.NewInt(100))
	ob1.ReleaseDeadline = time.Now().Add(-5 * time.Minute)
	l.Open(ctx, tenant, ob1)

	// Obligation 2: deadline also passed, but more recent.
	ob2 := testObligation(2, tenant, big.NewInt(200))
	ob2.ReleaseDeadline = time.Now().Add(-1 * time.Minute)
	l.Open(ctx, tenant, ob2)

	// Obligation 3: deadline in the future — should NOT appear.
	ob3 := testObligation(3, tenant, big.NewInt(300))
	ob3.ReleaseDeadline = time.Now().Add(1 * time.Hour)
	l.Open(ctx, tenant, ob3)

	// Obligation 4: already settled — should NOT appear.
	ob4 := testObligation(4, tenant, big.NewInt(400))
	ob4.ReleaseDeadline = time.Now().Add(-2 * time.Minute)
	ob4.Status = types.StatusReleased
	l.Open(ctx, tenant, ob4)

	pending, err := l.Pending(ctx, tenant)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}

	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}

	// Oldest first: ob1 (deadline -5m) before ob2 (deadline -1m).
	if pending[0].EscrowID != 1 {
		t.Fatalf("expected first pending to be escrow 1, got %d", pending[0].EscrowID)
	}
	if pending[1].EscrowID != 2 {
		t.Fatalf("expected second pending to be escrow 2, got %d", pending[1].EscrowID)
	}
}

// ---------------------------------------------------------------------------
// Test: Snapshot aggregation
// ---------------------------------------------------------------------------

func TestSnapshot(t *testing.T) {
	l := testLedger(t)
	ctx := context.Background()
	tenant := types.TenantID("t1")

	// 3 locked obligations.
	for i := 1; i <= 3; i++ {
		l.Open(ctx, tenant, testObligation(types.EscrowID(i), tenant, big.NewInt(1000)))
	}

	// Settle one as refunded.
	l.MarkSettled(ctx, tenant, 1, types.StatusRefunded, "0x111")

	// Settle one as released.
	l.MarkSettled(ctx, tenant, 2, types.StatusReleased, "0x222")

	snap, err := l.Snapshot(ctx, tenant)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// locked: only #3 = 1000
	if snap.Locked.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("Locked = %s, want 1000", snap.Locked)
	}
	// recovered: only #1 (refunded) = 1000
	if snap.Recovered.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("Recovered = %s, want 1000", snap.Recovered)
	}
	// pendingCount: only #3
	if snap.PendingCount != 1 {
		t.Fatalf("PendingCount = %d, want 1", snap.PendingCount)
	}
}

// ---------------------------------------------------------------------------
// Test: Schema idempotency (re-open same DB)
// ---------------------------------------------------------------------------

func TestSchemaIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reopen.db")

	l1, err := New(path)
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	ctx := context.Background()
	l1.Open(ctx, types.TenantID("t1"), testObligation(1, "t1", big.NewInt(100)))
	l1.Close()

	// Re-opening should not lose data or error on CREATE IF NOT EXISTS.
	l2, err := New(path)
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	defer l2.Close()

	snap, _ := l2.Snapshot(ctx, types.TenantID("t1"))
	if snap.PendingCount != 1 {
		t.Fatalf("data lost after reopen: pendingCount=%d", snap.PendingCount)
	}
}

// Suppress unused import warning.
var _ = os.DevNull
