package mocks

import (
	"context"
	"sync"
	"time"

	"github.com/A1kartikey/leash/internal/types"
)

// LedgerCall records a single invocation of a Ledger method.
type LedgerCall struct {
	Method string
	Args   []any
}

// MockLedger implements types.Ledger with injectable return values.
type MockLedger struct {
	mu    sync.Mutex
	Calls []LedgerCall

	// Return-value stubs — set these before exercising the code under test.
	OpenFn          func(ctx context.Context, tenant types.TenantID, ob types.Obligation) error
	MarkDeliveredFn func(ctx context.Context, tenant types.TenantID, id types.EscrowID, at time.Time) error
	MarkSettledFn   func(ctx context.Context, tenant types.TenantID, id types.EscrowID, status types.Status, tx types.TxHash) error
	PendingFn       func(ctx context.Context, tenant types.TenantID) ([]types.Obligation, error)
	SnapshotFn      func(ctx context.Context, tenant types.TenantID) (types.Balances, error)
}

var _ types.Ledger = (*MockLedger)(nil)

func (m *MockLedger) record(method string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, LedgerCall{Method: method, Args: args})
}

func (m *MockLedger) Open(ctx context.Context, tenant types.TenantID, ob types.Obligation) error {
	m.record("Open", tenant, ob)
	if m.OpenFn != nil {
		return m.OpenFn(ctx, tenant, ob)
	}
	return nil
}

func (m *MockLedger) MarkDelivered(ctx context.Context, tenant types.TenantID, id types.EscrowID, at time.Time) error {
	m.record("MarkDelivered", tenant, id, at)
	if m.MarkDeliveredFn != nil {
		return m.MarkDeliveredFn(ctx, tenant, id, at)
	}
	return nil
}

func (m *MockLedger) MarkSettled(ctx context.Context, tenant types.TenantID, id types.EscrowID, status types.Status, tx types.TxHash) error {
	m.record("MarkSettled", tenant, id, status, tx)
	if m.MarkSettledFn != nil {
		return m.MarkSettledFn(ctx, tenant, id, status, tx)
	}
	return nil
}

func (m *MockLedger) Pending(ctx context.Context, tenant types.TenantID) ([]types.Obligation, error) {
	m.record("Pending", tenant)
	if m.PendingFn != nil {
		return m.PendingFn(ctx, tenant)
	}
	return nil, nil
}

func (m *MockLedger) Snapshot(ctx context.Context, tenant types.TenantID) (types.Balances, error) {
	m.record("Snapshot", tenant)
	if m.SnapshotFn != nil {
		return m.SnapshotFn(ctx, tenant)
	}
	return types.Balances{}, nil
}
