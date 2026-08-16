// Package tenant holds the per-agent isolation boundary: each tenant has its
// own signer, and there is deliberately no way to reach one without naming a
// tenant.
//
// The registry maps a TenantID to that tenant's own chain handle. Building the
// handle is the caller's job precisely because that is where the key lives —
// one handle per key, never one handle shared by several tenants. A lookup for
// an unregistered tenant is an error, never a fallback to somebody else's
// signer.
//
// Ledger rows and breaker counters are already scoped by TenantID by the
// packages that own them; this package is the third leg, the signer.
package tenant

import (
	"fmt"
	"sort"
	"sync"

	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum/common"
)

// Tenant is one agent identity: an id and the signer that acts for it.
type Tenant struct {
	ID types.TenantID

	// Signer is the address the tenant's key controls. Exported for display;
	// the key itself never leaves the chain handle.
	Signer common.Address

	// chain is unexported so the only route to a signer is Registry.Chain,
	// which requires a TenantID.
	chain types.Chain
}

// Registry is the set of tenants one Leash process serves.
type Registry struct {
	mu      sync.RWMutex
	tenants map[types.TenantID]*Tenant
}

func New() *Registry {
	return &Registry{tenants: make(map[types.TenantID]*Tenant)}
}

// Add registers a tenant with its own chain handle.
//
// Passing one handle to two tenants defeats the whole boundary, so a handle
// already held by another tenant is rejected: the mistake is worth catching at
// startup rather than discovering in a settlement.
func (r *Registry) Add(id types.TenantID, signer common.Address, c types.Chain) error {
	if id == "" {
		return fmt.Errorf("tenant: empty tenant id")
	}
	if c == nil {
		return fmt.Errorf("tenant: %s has no chain handle", id)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tenants[id]; exists {
		return fmt.Errorf("tenant: %s is already registered", id)
	}
	for otherID, other := range r.tenants {
		if other.chain == c {
			return fmt.Errorf("tenant: %s would share a signer with %s", id, otherID)
		}
	}

	r.tenants[id] = &Tenant{ID: id, Signer: signer, chain: c}
	return nil
}

// Chain returns the tenant's own chain handle. An unknown tenant is an error:
// there is no default signer to fall back to.
func (r *Registry) Chain(id types.TenantID) (types.Chain, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.tenants[id]
	if !ok {
		return nil, fmt.Errorf("tenant: %q is not registered", id)
	}
	return t.chain, nil
}

// ChainFor is the resolver the engine takes, so the engine never holds a
// signer of its own.
func (r *Registry) ChainFor() func(types.TenantID) (types.Chain, error) {
	return r.Chain
}

// Signer reports which address acts for a tenant.
func (r *Registry) Signer(id types.TenantID) (common.Address, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.tenants[id]
	if !ok {
		return common.Address{}, fmt.Errorf("tenant: %q is not registered", id)
	}
	return t.Signer, nil
}

// IDs lists registered tenants, sorted so the order is stable. It is what the
// sweeper iterates each tick, which is why it is read fresh rather than
// snapshotted at startup: a tenant added at runtime gets swept too.
func (r *Registry) IDs() []types.TenantID {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]types.TenantID, 0, len(r.tenants))
	for id := range r.tenants {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
