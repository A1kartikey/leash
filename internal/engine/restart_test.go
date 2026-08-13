package engine

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"math/big"

	"github.com/A1kartikey/leash/internal/ledger"
	"github.com/A1kartikey/leash/internal/types"
	"github.com/A1kartikey/leash/internal/types/mocks"
	"github.com/ethereum/go-ethereum/common"
)

// Durability is a property, not a claim: lock N escrows, throw away every
// piece of in-memory state (the crash), reopen only the SQLite file, and
// assert the sweeper still refunds all N against their ORIGINAL deadlines.
// No recovery code path exists — the sweeper just finds them on the next tick.
func TestRestartRecovery(t *testing.T) {
	const n = 4
	path := filepath.Join(t.TempDir(), "obligations.db")
	deadline := time.Unix(1_700_000_000, 0)
	after := deadline.Add(time.Hour)

	// --- process 1: lock, then die before anything settles ------------------
	led, err := ledger.New(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := merchantServer(t, types.Challenge{
		Price: price, Merchant: merchantA, ResourceHash: hashOf("never sent"),
	}, 200, "application/json", "") // absent every time

	var nextID types.EscrowID
	chain := &mocks.MockChain{}
	chain.LockFn = func(context.Context, common.Address, *big.Int, [32]byte) (types.EscrowID, types.TxHash, error) {
		nextID++
		return nextID, "0xlock", nil
	}

	cfg := DefaultConfig()
	cfg.TTL = 0 // deadline == locked_at
	cfg.BreakerThreshold = n + 1
	e1 := New(SingleChain(chain), led, Verifier{}, cfg)
	e1.now = func() time.Time { return deadline }

	for i := 0; i < n; i++ {
		res, err := fetch(t, e1, srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		if res.Verdict.Outcome != types.VerdictAbsent {
			t.Fatalf("setup: verdict = %+v", res.Verdict)
		}
	}
	led.Close() // kill -9: every byte of in-memory state is gone

	// --- process 2: same file, brand new everything -------------------------
	led2, err := ledger.New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer led2.Close()

	chain2 := &mocks.MockChain{}
	e2 := New(SingleChain(chain2), led2, Verifier{}, cfg)
	e2.now = func() time.Time { return after }

	swept, err := e2.Sweep(context.Background(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	if swept != n {
		t.Fatalf("swept %d after restart, want %d", swept, n)
	}

	pending, err := led2.Pending(context.Background(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("%d obligations still locked after the sweep", len(pending))
	}

	snap, err := led2.Snapshot(context.Background(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	wantRecovered := new(big.Int).Mul(price, big.NewInt(n))
	if snap.Recovered.Cmp(wantRecovered) != 0 {
		t.Fatalf("recovered %s wei, want %s", snap.Recovered, wantRecovered)
	}
	if snap.Locked.Sign() != 0 {
		t.Fatalf("still locked: %s wei", snap.Locked)
	}
}
