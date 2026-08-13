// Package mocks provides test doubles for the core Leash interfaces.
// Every mock records its calls and lets the caller inject return values.
package mocks

import (
	"context"
	"math/big"
	"sync"

	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum/common"
)

// ChainCall records a single invocation of a Chain method.
type ChainCall struct {
	Method string
	Args   []any
}

// MockChain implements types.Chain with injectable return values.
type MockChain struct {
	mu    sync.Mutex
	Calls []ChainCall

	// Return-value stubs — set these before exercising the code under test.
	LockFn           func(ctx context.Context, merchant common.Address, amount *big.Int, resourceHash [32]byte) (types.EscrowID, types.TxHash, error)
	ReleaseFn        func(ctx context.Context, id types.EscrowID) (types.TxHash, error)
	RefundFn         func(ctx context.Context, id types.EscrowID) (types.TxHash, error)
	ReleasePartialFn func(ctx context.Context, id types.EscrowID, amount *big.Int) (types.TxHash, error)
	SubscribeFn      func(ctx context.Context) (<-chan types.EscrowEvent, error)
	BalanceOfFn      func(ctx context.Context, addr common.Address) (*big.Int, error)
}

var _ types.Chain = (*MockChain)(nil)

func (m *MockChain) record(method string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, ChainCall{Method: method, Args: args})
}

func (m *MockChain) Lock(ctx context.Context, merchant common.Address, amount *big.Int, resourceHash [32]byte) (types.EscrowID, types.TxHash, error) {
	m.record("Lock", merchant, amount, resourceHash)
	if m.LockFn != nil {
		return m.LockFn(ctx, merchant, amount, resourceHash)
	}
	return 1, "0xdead", nil
}

func (m *MockChain) Release(ctx context.Context, id types.EscrowID) (types.TxHash, error) {
	m.record("Release", id)
	if m.ReleaseFn != nil {
		return m.ReleaseFn(ctx, id)
	}
	return "0xbeef", nil
}

func (m *MockChain) Refund(ctx context.Context, id types.EscrowID) (types.TxHash, error) {
	m.record("Refund", id)
	if m.RefundFn != nil {
		return m.RefundFn(ctx, id)
	}
	return "0xcafe", nil
}

func (m *MockChain) ReleasePartial(ctx context.Context, id types.EscrowID, amount *big.Int) (types.TxHash, error) {
	m.record("ReleasePartial", id, amount)
	if m.ReleasePartialFn != nil {
		return m.ReleasePartialFn(ctx, id, amount)
	}
	return "0xfade", nil
}

func (m *MockChain) Subscribe(ctx context.Context) (<-chan types.EscrowEvent, error) {
	m.record("Subscribe")
	if m.SubscribeFn != nil {
		return m.SubscribeFn(ctx)
	}
	ch := make(chan types.EscrowEvent)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func (m *MockChain) BalanceOf(ctx context.Context, addr common.Address) (*big.Int, error) {
	m.record("BalanceOf", addr)
	if m.BalanceOfFn != nil {
		return m.BalanceOfFn(ctx, addr)
	}
	return big.NewInt(0), nil
}
