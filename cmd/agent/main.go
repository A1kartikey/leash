// Command agent is a throwaway demo buyer: a loop that retries on failure.
//
// The retry is the point. A merchant that takes the lock and returns nothing
// costs an unprotected agent the full price on every attempt — loss compounds
// with the retry count. That is the protocol's own documented failure
// behaviour, and it is what the escrow + sweeper exist to bound.
//
// Two shapes:
//
//	-proxy http://localhost:8080   buy through the Leash sidecar, so the
//	                               dashboard's ledger is the one being written
//	(default)                      run its own engine + ledger, standalone
//
// Pace and pause are stage controls: requests fire every -pace, and the loop
// can be held between demo beats without killing the process.
//
//	go run ./cmd/mockmerchant &
//	go run ./cmd/agent -mock -n 0 -pace 1500ms -control :8082
//	curl -X POST localhost:8082/toggle     # pause / resume
//	curl -X POST localhost:8081/mode -d fail
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
	proxy := flag.String("proxy", "", "buy through this Leash proxy instead of running an engine (e.g. http://localhost:8080)")
	tenant := flag.String("tenant", "agent-1", "tenant id — scopes every ledger row and breaker counter")
	dbPath := flag.String("db", filepath.Join(os.TempDir(), "leash-agent.db"), "obligation ledger path")
	n := flag.Int("n", 5, "number of resources to buy; 0 buys until stopped")
	retries := flag.Int("retries", 2, "retries per resource after a non-delivery")
	pace := flag.Duration("pace", 1500*time.Millisecond, "delay between requests — fast enough to watch, slow enough to read")
	control := flag.String("control", "", "listen address for pause/resume control (e.g. :8082)")
	ttl := flag.Duration("ttl", 30*time.Second, "escrow TTL (must match LEASH_DEFAULT_TTL on the real chain)")
	useMock := flag.Bool("mock", false, "use an in-memory chain instead of Monad testnet")
	flag.Parse()

	log.SetFlags(log.Ltime)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	loop := &loop{pace: *pace}
	if *control != "" {
		go loop.serve(ctx, *control)
	}

	if *proxy != "" {
		buyThroughProxy(ctx, loop, strings.TrimRight(*proxy, "/")+pathOf(*url), *n, *retries)
		return
	}

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

	loop.run(ctx, *n, *retries, func(i, try int) bool {
		res, err := buy(ctx, eng, tid, *url)
		switch {
		case errors.Is(err, engine.ErrCircuitOpen):
			log.Printf("resource %d try %d: CIRCUIT OPEN — 503, no funds locked", i, try)
			return true // stop retrying a merchant we have cut off
		case err != nil:
			log.Printf("resource %d try %d: %v", i, try, err)
			return false
		default:
			log.Printf("resource %d try %d: %-9s %-16s escrow=%d settle=%s",
				i, try, res.Verdict.Outcome, res.Verdict.Reason, res.EscrowID, short(string(res.SettleTx)))
			paidOut.Add(paidOut, merchantShare(cfg, res))
			if res.Verdict.Outcome == types.VerdictDelivered {
				loop.delivered++
				return true
			}
			return false
		}
	})

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
		tid, loop.attempts, loop.delivered, paidOut, snap.Recovered, snap.Locked, snap.PendingCount)
}

// ---------------------------------------------------------------------------
// The loop: paced, pausable, and the same shape in both modes
// ---------------------------------------------------------------------------

type loop struct {
	pace      time.Duration
	paused    atomic.Bool
	attempts  int
	delivered int
}

// run drives resources × retries, one attempt per pace tick, holding while
// paused. attempt reports whether this resource is done (delivered, or not
// worth retrying).
func (l *loop) run(ctx context.Context, n, retries int, attempt func(i, try int) bool) {
	for i := 0; (n == 0 || i < n) && ctx.Err() == nil; i++ {
		for try := 0; try <= retries; try++ {
			if !l.wait(ctx) {
				return
			}
			l.attempts++
			if attempt(i, try) {
				break
			}
		}
	}
}

// wait blocks while paused and then paces the next request. It returns false
// once the context is done.
func (l *loop) wait(ctx context.Context) bool {
	announced := false
	for l.paused.Load() {
		if !announced {
			log.Print("PAUSED — curl -X POST <control>/resume to continue")
			announced = true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
	if announced {
		log.Print("RESUMED")
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(l.pace):
		return true
	}
}

// serve exposes the stage controls. Pausing between beats beats killing the
// process: the ledger, the sweeper and the escrows all stay live.
func (l *loop) serve(ctx context.Context, addr string) {
	mux := http.NewServeMux()
	set := func(v bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			l.paused.Store(v)
			fmt.Fprintln(w, l.status())
		}
	}
	mux.HandleFunc("POST /pause", set(true))
	mux.HandleFunc("POST /resume", set(false))
	mux.HandleFunc("POST /toggle", func(w http.ResponseWriter, r *http.Request) {
		l.paused.Store(!l.paused.Load())
		log.Printf("control: %s", l.status())
		fmt.Fprintln(w, l.status())
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s attempts=%d delivered=%d\n", l.status(), l.attempts, l.delivered)
	})

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() { <-ctx.Done(); srv.Close() }()
	log.Printf("control on %s: POST /pause /resume /toggle, GET /status", addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("control: %v", err)
	}
}

func (l *loop) status() string {
	if l.paused.Load() {
		return "paused"
	}
	return "running"
}

// ---------------------------------------------------------------------------
// Buying
// ---------------------------------------------------------------------------

func buy(ctx context.Context, eng *engine.Engine, tid types.TenantID, url string) (*engine.Result, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return eng.Fetch(ctx, tid, req)
}

// buyThroughProxy is the demo rig's shape: the sidecar owns the keys, the
// ledger and the verdict, and the agent is just a client that keeps asking.
// The verdict comes back on the response headers Leash sets.
func buyThroughProxy(ctx context.Context, l *loop, url string, n, retries int) {
	client := &http.Client{Timeout: 60 * time.Second}
	log.Printf("buying through the Leash proxy: %s", url)

	l.run(ctx, n, retries, func(i, try int) bool {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			log.Printf("resource %d try %d: %v", i, try, err)
			return true
		}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("resource %d try %d: %v", i, try, err)
			return false
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()

		if resp.StatusCode == http.StatusServiceUnavailable {
			log.Printf("resource %d try %d: CIRCUIT OPEN — 503, no funds locked", i, try)
			return true
		}
		verdict := resp.Header.Get("X-Leash-Verdict")
		if verdict == "" {
			log.Printf("resource %d try %d: %d, %d bytes (unpaid)", i, try, resp.StatusCode, len(body))
			return true
		}
		log.Printf("resource %d try %d: %-9s %-16s %d, %d bytes",
			i, try, verdict, resp.Header.Get("X-Leash-Reason"), resp.StatusCode, len(body))
		if verdict == string(types.VerdictDelivered) {
			l.delivered++
			return true
		}
		return false
	})
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

// pathOf keeps the resource path when the request is redirected through a
// proxy: -url .../resource plus -proxy :8080 becomes :8080/resource.
func pathOf(rawURL string) string {
	if i := strings.Index(rawURL, "://"); i >= 0 {
		if j := strings.Index(rawURL[i+3:], "/"); j >= 0 {
			return rawURL[i+3+j:]
		}
		return "/"
	}
	if strings.HasPrefix(rawURL, "/") {
		return rawURL
	}
	return "/" + rawURL
}

func short(tx string) string {
	if len(tx) <= 12 {
		return tx
	}
	return tx[:8] + "…" + tx[len(tx)-4:]
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
