package ledger

import (
	"context"
	"sync"
	"time"

	"github.com/A1kartikey/leash/internal/types"
)

// Event is one committed ledger state change, as it happened.
//
// Events are derived from the ledger write itself — Notifier publishes only
// after the row is committed, and reads the committed row back for the
// details. There is deliberately no second path that can report a state the
// ledger does not hold.
type Event struct {
	Kind       string         `json:"kind"` // lock | released | refunded | partial
	Tenant     types.TenantID `json:"tenant"`
	EscrowID   types.EscrowID `json:"escrow_id"`
	Merchant   string         `json:"merchant"`
	Amount     string         `json:"amount"` // wei, decimal string — never a JSON number
	ResourceID string         `json:"resource_id"`
	Status     types.Status   `json:"status"`
	Tx         types.TxHash   `json:"tx"` // lock tx for a lock, settle tx otherwise
	At         time.Time      `json:"at"`
}

// Feed fans one event out to every live subscriber (an SSE connection each).
type Feed struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

func NewFeed() *Feed {
	return &Feed{subs: make(map[chan Event]struct{})}
}

// Subscribe returns a channel of events and a function to release it. The
// caller must call cancel or the subscription leaks.
func (f *Feed) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)

	f.mu.Lock()
	f.subs[ch] = struct{}{}
	f.mu.Unlock()

	return ch, func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if _, ok := f.subs[ch]; ok {
			delete(f.subs, ch)
			close(ch)
		}
	}
}

// Publish is non-blocking: a subscriber that cannot keep up drops events
// rather than stalling the settlement path. A dashboard is never allowed to
// hold up a refund.
func (f *Feed) Publish(e Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for ch := range f.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Notifier wraps a ledger so every committed state change is published to a
// Feed. It is the only event source: the engine and the sweeper already write
// here, so nothing has to remember to also notify.
type Notifier struct {
	*SQLiteLedger
	feed *Feed
}

var _ types.Ledger = (*Notifier)(nil)

// Observe wraps led so its writes publish to feed.
func Observe(led *SQLiteLedger, feed *Feed) *Notifier {
	return &Notifier{SQLiteLedger: led, feed: feed}
}

func (n *Notifier) Feed() *Feed { return n.feed }

func (n *Notifier) Open(ctx context.Context, tenant types.TenantID, ob types.Obligation) error {
	if err := n.SQLiteLedger.Open(ctx, tenant, ob); err != nil {
		return err
	}
	n.publish(ctx, tenant, ob.EscrowID, "lock")
	return nil
}

func (n *Notifier) MarkSettled(ctx context.Context, tenant types.TenantID, id types.EscrowID, status types.Status, tx types.TxHash) error {
	if err := n.SQLiteLedger.MarkSettled(ctx, tenant, id, status, tx); err != nil {
		return err
	}
	n.publish(ctx, tenant, id, string(status))
	return nil
}

// publish reads the committed row back so the event carries exactly what the
// ledger holds. A read failure costs a dashboard row, never a settlement.
func (n *Notifier) publish(ctx context.Context, tenant types.TenantID, id types.EscrowID, kind string) {
	ob, err := n.SQLiteLedger.Get(ctx, tenant, id)
	if err != nil {
		return
	}
	tx := ob.SettleTx
	if kind == "lock" {
		tx = ob.LockTx
	}
	n.feed.Publish(Event{
		Kind:       kind,
		Tenant:     tenant,
		EscrowID:   ob.EscrowID,
		Merchant:   ob.Merchant.Hex(),
		Amount:     ob.Amount.String(),
		ResourceID: ob.ResourceID,
		Status:     ob.Status,
		Tx:         tx,
		At:         time.Now().UTC(),
	})
}
