// Command leash is the sidecar proxy and its operator dashboard.
//
// Every request that is not a dashboard route is forwarded upstream through
// the settlement path: 402 -> lock -> retry -> verify -> release / refund.
// The dashboard reads the same ledger the sweeper writes to, and the event
// stream is published by the ledger itself — there is no second source of
// truth to drift.
//
//	go run ./cmd/mockmerchant &
//	go run ./cmd/leash -mock -upstream http://localhost:8081
//	open http://localhost:8080
//	curl localhost:8080/resource
package main

import (
	"context"
	"encoding/json"
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
	"time"

	"github.com/A1kartikey/leash/internal/chain"
	"github.com/A1kartikey/leash/internal/engine"
	"github.com/A1kartikey/leash/internal/ledger"
	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum/common"
)

func main() {
	addr := flag.String("addr", ":8080", "dashboard + proxy listen address")
	upstream := flag.String("upstream", "http://localhost:8081", "origin every non-dashboard request is forwarded to")
	tenant := flag.String("tenant", "agent-1", "tenant id — scopes every ledger row and breaker counter")
	dbPath := flag.String("db", filepath.Join(os.TempDir(), "leash.db"), "obligation ledger path")
	ttl := flag.Duration("ttl", 60*time.Second, "escrow TTL (must match LEASH_DEFAULT_TTL on the real chain)")
	mock := flag.Bool("mock", false, "use an in-memory chain instead of Monad testnet")
	explorer := flag.String("explorer", "https://testnet.monadexplorer.com", "block explorer base url")
	refresh := flag.Duration("refresh", 2*time.Second, "how often balances are re-read from chain + ledger")
	webDir := flag.String("web", "web", "directory served as the dashboard (plain HTML, no build step)")

	// Settlement knobs, defaulted to the production config. The demo profile
	// overrides them on the command line; DefaultConfig() itself never moves.
	def := engine.DefaultConfig()
	sweepInterval := flag.Duration("sweep-interval", 5*time.Second, "how often the sweeper polls for expired escrows")
	sweepGrace := flag.Duration("sweep-grace", 5*time.Second, "delay past the local deadline before refunding")
	breakerThreshold := flag.Int("breaker-threshold", def.BreakerThreshold, "consecutive non-deliveries before the circuit opens")
	breakerCooldown := flag.Duration("breaker-cooldown", def.BreakerCooldown, "how long the circuit stays open before a probe")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	store, err := ledger.New(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	// Every write to this ledger publishes to the feed. The engine and the
	// sweeper need no knowledge that a dashboard exists.
	feed := ledger.NewFeed()
	led := ledger.Observe(store, feed)

	ch, buyer, contract, err := openChain(ctx, *mock)
	if err != nil {
		log.Fatal(err)
	}

	cfg := def
	cfg.TTL = *ttl
	cfg.SweepInterval = *sweepInterval
	cfg.SweepGrace = *sweepGrace
	cfg.BreakerThreshold = *breakerThreshold
	cfg.BreakerCooldown = *breakerCooldown
	eng := engine.New(engine.SingleChain(ch), led, engine.Verifier{}, cfg)

	tid := types.TenantID(*tenant)
	go eng.RunSweeper(ctx, func() []types.TenantID { return []types.TenantID{tid} })

	d := &dashboard{
		eng: eng, chain: ch, store: store, feed: feed, cfg: cfg,
		tenant: tid, buyer: buyer, contract: contract,
		explorer: strings.TrimRight(*explorer, "/"), refresh: *refresh,
	}

	mux := http.NewServeMux()
	mux.Handle("GET /events", http.HandlerFunc(d.events))
	mux.Handle("GET /api/state", http.HandlerFunc(d.state))
	// The dashboard is one static file. Everything else on GET is upstream
	// traffic and goes through the settlement path.
	ui := http.FileServer(http.Dir(*webDir))
	mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, ".html") ||
			strings.HasSuffix(r.URL.Path, ".css") || strings.HasSuffix(r.URL.Path, ".js") {
			ui.ServeHTTP(w, r)
			return
		}
		d.proxy(w, r, *upstream)
	}))
	// Anything with a method other than GET is upstream traffic by definition.
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.proxy(w, r, *upstream)
	}))

	srv := &http.Server{Addr: *addr, Handler: mux}
	go func() {
		<-ctx.Done()
		sd, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(sd)
	}()

	log.SetFlags(log.Ltime)
	log.Printf("leash on %s: tenant=%s buyer=%s upstream=%s contract=%s", *addr, tid, buyer.Hex(), *upstream, contract.Hex())
	log.Printf("settlement: ttl=%s sweep=%s+%s breaker=%d/%s", cfg.TTL, cfg.SweepInterval, cfg.SweepGrace, cfg.BreakerThreshold, cfg.BreakerCooldown)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

type dashboard struct {
	eng      *engine.Engine
	chain    types.Chain
	store    *ledger.SQLiteLedger
	feed     *ledger.Feed
	cfg      engine.Config
	tenant   types.TenantID
	buyer    common.Address
	contract common.Address
	explorer string
	refresh  time.Duration
}

// ---------------------------------------------------------------------------
// Proxy
// ---------------------------------------------------------------------------

func (d *dashboard) proxy(w http.ResponseWriter, r *http.Request, upstream string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "leash: reading request body", http.StatusBadRequest)
		return
	}
	r.Body.Close()

	target := strings.TrimRight(upstream, "/") + r.URL.RequestURI()
	req, err := http.NewRequest(r.Method, target, strings.NewReader(string(body)))
	if err != nil {
		http.Error(w, "leash: bad upstream url", http.StatusBadGateway)
		return
	}
	req.Header = r.Header.Clone()

	res, err := d.eng.Fetch(r.Context(), d.tenant, req)
	switch {
	case errors.Is(err, engine.ErrCircuitOpen):
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	case err != nil && res == nil:
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	for k, vs := range res.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	if res.Paid {
		w.Header().Set("X-Leash-Verdict", string(res.Verdict.Outcome))
		w.Header().Set("X-Leash-Reason", res.Verdict.Reason)
	}
	status := res.Status
	if status == 0 {
		status = http.StatusBadGateway
	}
	w.WriteHeader(status)
	w.Write(res.Body)
}

// ---------------------------------------------------------------------------
// State snapshot: chain + ledger, read fresh on every call
// ---------------------------------------------------------------------------

type merchantView struct {
	Address string `json:"address"`
	Balance string `json:"balance"` // wei
	Circuit string `json:"circuit"` // closed | open | half-open
}

type feedRow struct {
	Kind       string `json:"kind"`
	EscrowID   uint64 `json:"escrow_id"`
	Amount     string `json:"amount"`      // wei
	PaidAmount string `json:"paid_amount"` // wei that reached the merchant
	Merchant   string `json:"merchant"`
	ResourceID string `json:"resource_id"`
	Tx         string `json:"tx"`
}

type stateView struct {
	Tenant    string         `json:"tenant"`
	Buyer     string         `json:"buyer"`
	Contract  string         `json:"contract"`
	Explorer  string         `json:"explorer"`
	Spendable string         `json:"spendable"` // wei, on-chain
	Locked    string         `json:"locked"`    // wei, ledger
	Recovered string         `json:"recovered"` // wei, ledger
	Pending   int            `json:"pending"`
	Merchants []merchantView `json:"merchants"`
	Recent    []feedRow      `json:"recent,omitempty"`
}

// snapshot reads the on-chain balance and the ledger every time it is called.
// Nothing here is cached: a stale balance on this screen would misrepresent
// the one thing the screen exists to show.
//
// ponytail: one BalanceOf per merchant per refresh; batch it if a tenant ever
// has more than a handful of merchants.
func (d *dashboard) snapshot(ctx context.Context, withRecent bool) (stateView, error) {
	bal, err := d.store.Snapshot(ctx, d.tenant)
	if err != nil {
		return stateView{}, err
	}

	spendable := new(big.Int)
	if v, err := d.chain.BalanceOf(ctx, d.buyer); err == nil && v != nil {
		spendable = v
	}

	sv := stateView{
		Tenant:    string(d.tenant),
		Buyer:     d.buyer.Hex(),
		Contract:  d.contract.Hex(),
		Explorer:  d.explorer,
		Spendable: spendable.String(),
		Locked:    bal.Locked.String(),
		Recovered: bal.Recovered.String(),
		Pending:   bal.PendingCount,
	}

	merchants, err := d.store.Merchants(ctx, d.tenant)
	if err != nil {
		return stateView{}, err
	}
	for _, m := range merchants {
		mb := new(big.Int)
		if v, err := d.chain.BalanceOf(ctx, m); err == nil && v != nil {
			mb = v
		}
		sv.Merchants = append(sv.Merchants, merchantView{
			Address: m.Hex(),
			Balance: mb.String(),
			Circuit: string(d.eng.Breaker().State(d.tenant, m)),
		})
	}

	if withRecent {
		obs, err := d.store.Recent(ctx, d.tenant, 40)
		if err != nil {
			return stateView{}, err
		}
		for _, ob := range obs {
			sv.Recent = append(sv.Recent, d.row(ob))
		}
	}
	return sv, nil
}

// row renders a ledger obligation as a feed row. Status is the ledger's, not
// an interpretation of it.
func (d *dashboard) row(ob types.Obligation) feedRow {
	kind := string(ob.Status)
	if ob.Status == types.StatusLocked {
		kind = "lock"
	}
	tx := ob.SettleTx
	if tx == "" {
		tx = ob.LockTx
	}
	return feedRow{
		Kind:       kind,
		EscrowID:   uint64(ob.EscrowID),
		Amount:     ob.Amount.String(),
		PaidAmount: d.paid(ob.Status, ob.Amount).String(),
		Merchant:   ob.Merchant.Hex(),
		ResourceID: ob.ResourceID,
		Tx:         string(tx),
	}
}

// paid is what actually reached the merchant. The partial share is the same
// PartialBps the engine settled with — the ledger stores the escrow total.
func (d *dashboard) paid(status types.Status, amount *big.Int) *big.Int {
	switch status {
	case types.StatusReleased:
		return amount
	case types.StatusPartial:
		v := new(big.Int).Mul(amount, big.NewInt(d.cfg.PartialBps))
		return v.Div(v, big.NewInt(10_000))
	default:
		return new(big.Int)
	}
}

func (d *dashboard) state(w http.ResponseWriter, r *http.Request) {
	sv, err := d.snapshot(r.Context(), true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sv)
}

// ---------------------------------------------------------------------------
// SSE
// ---------------------------------------------------------------------------

// events streams two kinds of message on one connection:
//
//	ledger — one per committed ledger state change, published by the ledger
//	state  — the balance/circuit snapshot, on every change and on a timer
//
// The timer exists because on-chain balances and a tripped breaker move
// without a ledger write; the ledger events are what arrive "as it happens".
func (d *dashboard) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "leash: streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sub, cancel := d.feed.Subscribe()
	defer cancel()

	send := func(kind string, v any) bool {
		b, err := json.Marshal(v)
		if err != nil {
			return true
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	pushState := func() bool {
		sv, err := d.snapshot(r.Context(), false)
		if err != nil {
			return true // a failed read is not a reason to drop the stream
		}
		return send("state", sv)
	}

	if !pushState() {
		return
	}

	tick := time.NewTicker(d.refresh)
	defer tick.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, open := <-sub:
			if !open {
				return
			}
			if !send("ledger", d.eventRow(ev)) || !pushState() {
				return
			}
		case <-tick.C:
			if !pushState() {
				return
			}
		}
	}
}

// eventRow maps a ledger event onto the same shape the feed renders.
func (d *dashboard) eventRow(ev ledger.Event) feedRow {
	amount, ok := new(big.Int).SetString(ev.Amount, 10)
	if !ok {
		amount = new(big.Int)
	}
	kind := ev.Kind
	if kind == string(types.StatusLocked) {
		kind = "lock"
	}
	return feedRow{
		Kind:       kind,
		EscrowID:   uint64(ev.EscrowID),
		Amount:     amount.String(),
		PaidAmount: d.paid(ev.Status, amount).String(),
		Merchant:   ev.Merchant,
		ResourceID: ev.ResourceID,
		Tx:         string(ev.Tx),
	}
}

// ---------------------------------------------------------------------------
// Chain
// ---------------------------------------------------------------------------

func openChain(ctx context.Context, mock bool) (types.Chain, common.Address, common.Address, error) {
	if mock {
		c := newDemoChain()
		return c, c.buyer, common.HexToAddress("0x0000000000000000000000000000000000000000"), nil
	}
	cfg, err := chain.LoadConfig()
	if err != nil {
		return nil, common.Address{}, common.Address{}, fmt.Errorf("leash: %w (run with -mock for an offline demo)", err)
	}
	ec, err := chain.NewEscrowChain(ctx, cfg)
	if err != nil {
		return nil, common.Address{}, common.Address{}, err
	}
	return ec, cfg.SignerAddr, cfg.ContractAddr, nil
}
