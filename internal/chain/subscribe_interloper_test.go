//go:build integration

// A second, unrelated signer locking against the same public singleton is the
// one condition that makes ID-based assumptions fail, and it is invisible with
// a single signer: alone, your escrow IDs look contiguous and every event on
// the contract is yours. With two signers neither is true.
//
// Run:
//
//	MONAD_RPC=https://testnet-rpc.monad.xyz \
//	BUYER_KEY=<hex> MERCHANT_KEY=<hex> LEASH_DEFAULT_TTL=60 \
//	go test -v -tags integration -timeout 900s -run Interloper ./internal/chain/...
package chain_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/A1kartikey/leash/internal/chain"
	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum/common"
)

// lockOrFail locks one escrow and returns its ID, which comes from the
// EscrowLocked event in the receipt — never from nextId, never inferred.
func lockOrFail(t *testing.T, ctx context.Context, ec *chain.EscrowChain, who string, merchant common.Address) types.EscrowID {
	t.Helper()
	id, tx, err := ec.Lock(ctx, merchant, smallAmount(), randomHash())
	if err != nil {
		t.Fatalf("%s lock: %v", who, err)
	}
	t.Logf("%-10s locked escrow %-4d naming merchant %s (%s)", who, id, merchant.Hex(), tx)
	return id
}

// refundWhenAllowed retries until past the release deadline the chain enforces,
// so the test leaves no funds stranded in the singleton.
func refundWhenAllowed(t *testing.T, ctx context.Context, ec *chain.EscrowChain, id types.EscrowID, ttl time.Duration) {
	t.Helper()

	deadline := time.Now().Add(ttl + 3*time.Minute)
	for time.Now().Before(deadline) {
		if _, err := ec.Refund(ctx, id); err == nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
	t.Logf("escrow %d could not be refunded within the window; it stays claimable", id)
}

func TestSubscribeIgnoresInterloperEscrows(t *testing.T) {
	buyer, ctx, _ := testEnv(t)

	cfg, err := chain.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	ttl := time.Duration(cfg.DefaultTTL) * time.Second
	if ttl > 300*time.Second {
		t.Skipf("LEASH_DEFAULT_TTL=%ds is too long to refund inside this test; set it to 60", cfg.DefaultTTL)
	}
	buyerAddr := cfg.SignerAddr

	// The interloper is a second independent signer with its own nonce, its
	// own escrows, and no relationship to us beyond sharing the contract.
	interloper := merchantChain(t, ctx)
	interloperAddr := merchantAddr(t)
	if interloperAddr == buyerAddr {
		t.Skip("BUYER_KEY and MERCHANT_KEY are the same signer; this test needs two")
	}

	events, err := buyer.Subscribe(ctx)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	var (
		mu   sync.Mutex
		seen []types.EscrowEvent
	)
	go func() {
		for ev := range events {
			mu.Lock()
			seen = append(seen, ev)
			mu.Unlock()
			t.Logf("stream: %s escrow %d block %d", ev.Kind, ev.EscrowID, ev.BlockNumber)
		}
	}()

	// Interleaved, so the interloper takes an ID from between ours.
	b1 := lockOrFail(t, ctx, buyer, "buyer", randomAddress())
	i1 := lockOrFail(t, ctx, interloper, "interloper", randomAddress())
	b2 := lockOrFail(t, ctx, buyer, "buyer", randomAddress())

	// This one names *our* address as the merchant. EscrowLocked indexes buyer
	// and merchant in different topics, so an escrow that merely mentions us
	// must not read as an escrow of ours — this is the topic-position trap.
	i2 := lockOrFail(t, ctx, interloper, "interloper", buyerAddr)

	// EscrowReleased is indexed by merchant, not buyer, so the interloper
	// settling this escrow produces exactly the log a merchant-side filter
	// would hand us as if it were our own.
	if _, err := interloper.Release(ctx, i2); err != nil {
		t.Fatalf("interloper release: %v", err)
	}
	t.Logf("interloper released escrow %d, paying our address as its merchant", i2)

	// The interloper really did take an ID between ours. Without this the test
	// could pass on a contract nobody else is using, proving nothing.
	if !(b1 < i1 && i1 < b2) {
		t.Fatalf("ids did not interleave (buyer %d, %d; interloper %d, %d): "+
			"the ID-collision condition was never created", b1, b2, i1, i2)
	}
	if b2 == b1+1 {
		t.Fatalf("buyer ids %d and %d are contiguous; the interloper's escrow should sit between them", b1, b2)
	}
	t.Logf("ids interleaved: buyer %d, interloper %d, buyer %d, interloper %d", b1, i1, b2, i2)

	// Wait for our own two locks to arrive, which is also the positive control:
	// a subscription that returned nothing at all would pass a purity check.
	waitFor(t, &mu, &seen, map[types.EscrowID]bool{b1: true, b2: true}, types.EventLocked, 90*time.Second)

	assertNoInterloperEvents(t, &mu, &seen, i1, i2)

	// Our own release must still arrive. Releases cannot be filtered by buyer
	// at the log layer, so they are attributed from the locks we watched — and
	// an attribution rule that drops everything would satisfy the purity check
	// above while silently blinding the sidecar to its own settlements.
	b3 := lockOrFail(t, ctx, buyer, "buyer", interloperAddr)
	if _, err := buyer.Release(ctx, b3); err != nil {
		t.Fatalf("buyer release: %v", err)
	}
	waitFor(t, &mu, &seen, map[types.EscrowID]bool{b3: true}, types.EventReleased, 90*time.Second)

	// Every Locked event on the stream must be one of ours.
	mu.Lock()
	for _, ev := range seen {
		if ev.Kind == types.EventLocked && ev.EscrowID != b1 && ev.EscrowID != b2 && ev.EscrowID != b3 {
			mu.Unlock()
			t.Fatalf("stream carried a lock we did not make: escrow %d", ev.EscrowID)
		}
	}
	mu.Unlock()

	// Refunds are buyer-indexed, so they exercise the same guarantee on a
	// second event type — and they return the money.
	refundWhenAllowed(t, ctx, buyer, b1, ttl)
	refundWhenAllowed(t, ctx, buyer, b2, ttl)
	refundWhenAllowed(t, ctx, interloper, i1, ttl)

	waitFor(t, &mu, &seen, map[types.EscrowID]bool{b1: true, b2: true}, types.EventRefunded, 90*time.Second)
	assertNoInterloperEvents(t, &mu, &seen, i1, i2)
}

// waitFor blocks until every wanted escrow id has appeared with the given kind.
func waitFor(t *testing.T, mu *sync.Mutex, seen *[]types.EscrowEvent, want map[types.EscrowID]bool, kind types.EscrowEventKind, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		got := map[types.EscrowID]bool{}
		mu.Lock()
		for _, ev := range *seen {
			if ev.Kind == kind {
				got[ev.EscrowID] = true
			}
		}
		mu.Unlock()

		missing := false
		for id := range want {
			if !got[id] {
				missing = true
			}
		}
		if !missing {
			return
		}
		time.Sleep(2 * time.Second)
	}

	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("timed out waiting for %s events %v; stream held %+v", kind, keys(want), *seen)
}

// assertNoInterloperEvents is the assertion the whole test exists for.
func assertNoInterloperEvents(t *testing.T, mu *sync.Mutex, seen *[]types.EscrowEvent, ids ...types.EscrowID) {
	t.Helper()

	mu.Lock()
	defer mu.Unlock()
	for _, ev := range *seen {
		for _, id := range ids {
			if ev.EscrowID == id {
				t.Fatalf("subscription leaked another signer's escrow: %s for escrow %d", ev.Kind, id)
			}
		}
	}
}

func keys(m map[types.EscrowID]bool) []types.EscrowID {
	out := make([]types.EscrowID, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// randomAddress is an arbitrary merchant: unrelated to either signer.
func randomAddress() common.Address {
	var a common.Address
	h := randomHash()
	copy(a[:], h[:20])
	return a
}
