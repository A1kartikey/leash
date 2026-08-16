package ledger

import (
	"context"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum/common"
)

func observed(t *testing.T) (*Notifier, <-chan Event) {
	t.Helper()

	store, err := New(filepath.Join(t.TempDir(), "notify.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	feed := NewFeed()
	sub, cancel := feed.Subscribe()
	t.Cleanup(cancel)

	return Observe(store, feed), sub
}

func obligation() types.Obligation {
	return types.Obligation{
		EscrowID:        7,
		TenantID:        "agent-1",
		Merchant:        common.HexToAddress("0x000000000000000000000000000000000000dEaD"),
		Amount:          big.NewInt(50_000_000_000_000_000),
		ResourceID:      "weather/nyc",
		LockedAt:        time.Now(),
		ReleaseDeadline: time.Now().Add(time.Minute),
		Status:          types.StatusLocked,
		LockTx:          "0xlock",
	}
}

func next(t *testing.T, sub <-chan Event) Event {
	t.Helper()
	select {
	case ev := <-sub:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("no event published")
		return Event{}
	}
}

// Every committed write publishes, and the event carries what the ledger
// actually holds — that is the property that keeps the dashboard from
// drifting away from settlement state.
func TestNotifierPublishesLedgerWrites(t *testing.T) {
	n, sub := observed(t)
	ctx := context.Background()
	ob := obligation()

	if err := n.Open(ctx, ob.TenantID, ob); err != nil {
		t.Fatal(err)
	}
	ev := next(t, sub)
	if ev.Kind != "lock" || ev.EscrowID != ob.EscrowID {
		t.Fatalf("got %+v, want a lock for escrow %d", ev, ob.EscrowID)
	}
	if ev.Amount != ob.Amount.String() || ev.ResourceID != "weather/nyc" || ev.Tx != "0xlock" {
		t.Fatalf("lock event lost ledger detail: %+v", ev)
	}

	if err := n.MarkSettled(ctx, ob.TenantID, ob.EscrowID, types.StatusRefunded, "0xrefund"); err != nil {
		t.Fatal(err)
	}
	ev = next(t, sub)
	if ev.Kind != string(types.StatusRefunded) || ev.Status != types.StatusRefunded || ev.Tx != "0xrefund" {
		t.Fatalf("got %+v, want a refund for escrow %d", ev, ob.EscrowID)
	}
}

// A failed write must not be announced as if it happened.
func TestNotifierStaysSilentOnFailedWrite(t *testing.T) {
	n, sub := observed(t)

	if err := n.MarkSettled(context.Background(), "agent-1", 404, types.StatusReleased, "0xnope"); err == nil {
		t.Fatal("expected an error settling an escrow that does not exist")
	}
	select {
	case ev := <-sub:
		t.Fatalf("published %+v for a write that failed", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

// A dashboard that stops reading must never stall a refund.
func TestFeedPublishNeverBlocks(t *testing.T) {
	feed := NewFeed()
	_, cancel := feed.Subscribe() // subscribed and never drained
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			feed.Publish(Event{Kind: "lock"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a subscriber that is not reading")
	}
}
