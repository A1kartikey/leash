// Command agent is a throwaway demo buyer: a loop that retries on failure.
//
// The retry is the point. A merchant that takes the lock and returns nothing
// costs an unprotected agent the full price on every attempt — loss compounds
// with the retry count. That is the protocol's own documented failure
// behaviour, and it is what the escrow + sweeper exist to bound.
//
//	go run ./cmd/mockmerchant &
//	go run ./cmd/agent -mock -n 5
//	curl -X POST 'localhost:8081/mode?m=fail-after-settlement'   # watch the loss
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/A1kartikey/leash/internal/chain"
	"github.com/A1kartikey/leash/internal/engine"
	"github.com/A1kartikey/leash/internal/ledger"
	"github.com/A1kartikey/leash/internal/types"
	"github.com/A1kartikey/leash/internal/types/mocks"
	"github.com/ethereum/go-ethereum/common"
)

func main() {
	url := flag.String("url", "http://localhost:8081/resource", "paid resource to fetch")
	tenant := flag.String("tenant", "agent-1", "tenant id — scopes every ledger row and breaker counter")
	dbPath := flag.String("db", filepath.Join(os.TempDir(), "leash-agent.db"), "obligation ledger path")
	n := flag.Int("n", 5, "number of resources to buy")
	retries := flag.Int("retries", 2, "retries per resource after a non-delivery")
	ttl := flag.Duration("ttl", 30*time.Second, "escrow TTL (must match LEASH_DEFAULT_TTL on the real chain)")
	useMock := flag.Bool("mock", false, "use an in-memory chain instead of Monad testnet")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	led, err := ledger.New(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer led.Close()

	ch, err := openChain(ctx, *useMock)
	if err != nil {
		log.Fatal(err)
	}

	cfg := engine.DefaultConfig()
	cfg.TTL = *ttl
	cfg.SweepInterval = 3 * time.Second
	cfg.SweepGrace = 2 * time.Second
	eng := engine.New(engine.SingleChain(ch), led, engine.Verifier{}, cfg)

	tid := types.TenantID(*tenant)
	go eng.RunSweeper(ctx, func() []types.TenantID { return []types.TenantID{tid} })

	paidOut := new(big.Int) // wei that actually reached the merchant
	attempts, delivered := 0, 0

	for i := 0; i < *n && ctx.Err() == nil; i++ {
		for try := 0; try <= *retries; try++ {
			attempts++
			res, err := buy(ctx, eng, tid, *url)
			switch {
			case errors.Is(err, engine.ErrCircuitOpen):
				log.Printf("resource %d: circuit open — 503, no funds locked", i)
				try = *retries // stop retrying a merchant we have cut off
			case err != nil:
				log.Printf("resource %d attempt %d: %v", i, try, err)
			default:
				log.Printf("resource %d attempt %d: %s (%s) escrow=%d settle=%s",
					i, try, res.Verdict.Outcome, res.Verdict.Reason, res.EscrowID, res.SettleTx)
				paidOut.Add(paidOut, merchantShare(cfg, res))
				if res.Verdict.Outcome == types.VerdictDelivered {
					delivered++
					try = *retries // got what we paid for
				}
			}
		}
	}

	// Give the sweeper a window to refund what never arrived.
	log.Printf("waiting out the %s TTL so the sweeper can refund...", *ttl)
	select {
	case <-ctx.Done():
	case <-time.After(*ttl + 2*cfg.SweepInterval):
	}

	snap, err := led.Snapshot(context.Background(), tid)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n--- tenant %s ---\nattempts   %d\ndelivered  %d\npaid out   %s wei\nrecovered  %s wei\nstill locked %s wei (%d obligations)\n",
		tid, attempts, delivered, paidOut, snap.Recovered, snap.Locked, snap.PendingCount)
}

func buy(ctx context.Context, eng *engine.Engine, tid types.TenantID, url string) (*engine.Result, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return eng.Fetch(ctx, tid, req)
}

// merchantShare is what this result actually moved to the merchant. Absent
// moves nothing: the sweeper refunds it.
func merchantShare(cfg engine.Config, res *engine.Result) *big.Int {
	if res.Challenge == nil {
		return new(big.Int)
	}
	switch res.Verdict.Outcome {
	case types.VerdictDelivered:
		return res.Challenge.Price
	case types.VerdictPartial:
		v := new(big.Int).Mul(res.Challenge.Price, big.NewInt(cfg.PartialBps))
		return v.Div(v, big.NewInt(10_000))
	default:
		return new(big.Int)
	}
}

// openChain returns the real Monad testnet wrapper, or an in-memory stand-in
// so the demo runs without funds.
func openChain(ctx context.Context, mock bool) (types.Chain, error) {
	if !mock {
		cfg, err := chain.LoadConfig()
		if err != nil {
			return nil, fmt.Errorf("agent: %w (run with -mock for an offline demo)", err)
		}
		return chain.NewEscrowChain(ctx, cfg)
	}

	var nextID atomic.Uint64
	return &mocks.MockChain{
		LockFn: func(context.Context, common.Address, *big.Int, [32]byte) (types.EscrowID, types.TxHash, error) {
			id := nextID.Add(1)
			return types.EscrowID(id), types.TxHash(fmt.Sprintf("0xmocklock%d", id)), nil
		},
	}, nil
}
