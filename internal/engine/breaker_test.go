package engine

import (
	"errors"
	"testing"
	"time"

	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum/common"
)

var (
	merchantA = common.HexToAddress("0x00000000000000000000000000000000000000aa")
	merchantB = common.HexToAddress("0x00000000000000000000000000000000000000bb")
)

func testBreaker(t *testing.T, threshold int, cooldown time.Duration) (*Breaker, *time.Time) {
	t.Helper()
	clock := time.Unix(1_700_000_000, 0)
	b := NewBreaker(threshold, cooldown)
	b.now = func() time.Time { return clock }
	return b, &clock
}

func TestBreakerOpensAtThreshold(t *testing.T) {
	b, _ := testBreaker(t, 3, time.Minute)
	const tenant types.TenantID = "t1"

	for i := 0; i < 2; i++ {
		b.Failure(tenant, merchantA)
		if err := b.Allow(tenant, merchantA); err != nil {
			t.Fatalf("failure %d: circuit opened early: %v", i+1, err)
		}
	}
	b.Failure(tenant, merchantA)
	if err := b.Allow(tenant, merchantA); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("want ErrCircuitOpen at threshold, got %v", err)
	}
	if got := b.State(tenant, merchantA); got != BreakerOpen {
		t.Fatalf("state = %s, want open", got)
	}
}

func TestBreakerResetsOnSuccess(t *testing.T) {
	b, _ := testBreaker(t, 2, time.Minute)
	const tenant types.TenantID = "t1"

	b.Failure(tenant, merchantA)
	b.Success(tenant, merchantA) // consecutive only — this clears the count
	b.Failure(tenant, merchantA)

	if err := b.Allow(tenant, merchantA); err != nil {
		t.Fatalf("counter was not reset by success: %v", err)
	}
}

func TestBreakerHalfOpenProbe(t *testing.T) {
	b, clock := testBreaker(t, 1, time.Minute)
	const tenant types.TenantID = "t1"

	b.Failure(tenant, merchantA)
	if err := b.Allow(tenant, merchantA); !errors.Is(err, ErrCircuitOpen) {
		t.Fatal("want open before cooldown")
	}

	*clock = clock.Add(time.Minute)
	if got := b.State(tenant, merchantA); got != BreakerHalfOpen {
		t.Fatalf("state = %s, want half-open", got)
	}
	if err := b.Allow(tenant, merchantA); err != nil {
		t.Fatalf("probe should be allowed after cooldown: %v", err)
	}
	// Exactly one probe: the next caller is still refused.
	if err := b.Allow(tenant, merchantA); !errors.Is(err, ErrCircuitOpen) {
		t.Fatal("second concurrent probe should be refused")
	}

	// A failed probe re-arms the full cooldown.
	b.Failure(tenant, merchantA)
	if err := b.Allow(tenant, merchantA); !errors.Is(err, ErrCircuitOpen) {
		t.Fatal("failed probe should reopen the circuit")
	}

	// A successful probe closes it.
	*clock = clock.Add(time.Minute)
	if err := b.Allow(tenant, merchantA); err != nil {
		t.Fatalf("second probe: %v", err)
	}
	b.Success(tenant, merchantA)
	if got := b.State(tenant, merchantA); got != BreakerClosed {
		t.Fatalf("state = %s, want closed", got)
	}
}

func TestBreakerStateIsPerTenantAndMerchant(t *testing.T) {
	b, _ := testBreaker(t, 1, time.Minute)

	b.Failure("t1", merchantA)

	if err := b.Allow("t2", merchantA); err != nil {
		t.Fatalf("tenant t2 must not inherit t1's breaker: %v", err)
	}
	if err := b.Allow("t1", merchantB); err != nil {
		t.Fatalf("merchant B must not inherit merchant A's breaker: %v", err)
	}
	if err := b.Allow("t1", merchantA); !errors.Is(err, ErrCircuitOpen) {
		t.Fatal("t1/merchantA should be open")
	}
}
