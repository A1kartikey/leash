package mocks

import (
	"context"
	"net/http"
	"sync"

	"github.com/A1kartikey/leash/internal/types"
)

// VerifierCall records a single invocation of the Verify method.
type VerifierCall struct {
	Challenge types.Challenge
}

// MockVerifier implements types.Verifier with an injectable return value.
type MockVerifier struct {
	mu    sync.Mutex
	Calls []VerifierCall

	// VerifyFn is the stub called by Verify. When nil, returns VerdictDelivered.
	VerifyFn func(ctx context.Context, c types.Challenge, resp *http.Response) (types.Verdict, error)
}

var _ types.Verifier = (*MockVerifier)(nil)

func (m *MockVerifier) Verify(ctx context.Context, c types.Challenge, resp *http.Response) (types.Verdict, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, VerifierCall{Challenge: c})
	m.mu.Unlock()

	if m.VerifyFn != nil {
		return m.VerifyFn(ctx, c, resp)
	}
	return types.Verdict{
		Outcome: types.VerdictDelivered,
		Reason:  "mock: auto-delivered",
	}, nil
}
