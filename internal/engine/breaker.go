package engine

import (
	"errors"
	"sync"
	"time"

	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum/common"
)

// ErrCircuitOpen is returned before any funds are locked. Callers map it to
// HTTP 503.
var ErrCircuitOpen = errors.New("engine: circuit open for merchant")

// BreakerState is the externally visible state of one (tenant, merchant) pair.
type BreakerState string

const (
	BreakerClosed   BreakerState = "closed"
	BreakerOpen     BreakerState = "open"
	BreakerHalfOpen BreakerState = "half-open"
)

type breakerKey struct {
	tenant   types.TenantID
	merchant common.Address
}

type breakerEntry struct {
	fails    int
	openedAt time.Time
	probing  bool // a half-open probe is in flight
}

// Breaker counts consecutive non-deliveries per (tenant_id, merchant). A
// success resets the counter. State is per-tenant and never shared: two
// tenants buying from the same bad merchant trip independently.
type Breaker struct {
	mu        sync.Mutex
	threshold int
	cooldown  time.Duration
	now       func() time.Time
	entries   map[breakerKey]*breakerEntry
}

func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	if threshold < 1 {
		threshold = 1
	}
	return &Breaker{
		threshold: threshold,
		cooldown:  cooldown,
		now:       time.Now,
		entries:   make(map[breakerKey]*breakerEntry),
	}
}

// Allow is checked BEFORE any funds are locked. It returns ErrCircuitOpen
// while the circuit is open, and lets exactly one probe through once the
// cooldown has elapsed.
func (b *Breaker) Allow(tenant types.TenantID, merchant common.Address) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	e := b.entries[breakerKey{tenant, merchant}]
	if e == nil || e.fails < b.threshold {
		return nil
	}
	if e.probing || b.now().Sub(e.openedAt) < b.cooldown {
		return ErrCircuitOpen
	}
	e.probing = true // half-open: this caller is the probe
	return nil
}

// Success resets the counter. Only a Delivered verdict is a success.
func (b *Breaker) Success(tenant types.TenantID, merchant common.Address) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.entries, breakerKey{tenant, merchant})
}

// Failure records a non-delivery. Partial counts: it is not a delivery.
func (b *Breaker) Failure(tenant types.TenantID, merchant common.Address) {
	b.mu.Lock()
	defer b.mu.Unlock()

	k := breakerKey{tenant, merchant}
	e := b.entries[k]
	if e == nil {
		e = &breakerEntry{}
		b.entries[k] = e
	}
	e.fails++
	e.probing = false
	if e.fails >= b.threshold {
		// Re-arms the cooldown, so a failed probe reopens for a full window.
		e.openedAt = b.now()
	}
}

// State reports the current circuit state for display.
func (b *Breaker) State(tenant types.TenantID, merchant common.Address) BreakerState {
	b.mu.Lock()
	defer b.mu.Unlock()

	e := b.entries[breakerKey{tenant, merchant}]
	switch {
	case e == nil || e.fails < b.threshold:
		return BreakerClosed
	case e.probing || b.now().Sub(e.openedAt) < b.cooldown:
		return BreakerOpen
	default:
		return BreakerHalfOpen
	}
}
