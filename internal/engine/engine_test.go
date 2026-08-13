package engine

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/A1kartikey/leash/internal/types"
	"github.com/A1kartikey/leash/internal/types/mocks"
	"github.com/ethereum/go-ethereum/common"
)

const tenant types.TenantID = "agent-1"

var price = big.NewInt(1_000_000_000_000_000) // 0.001 MON

// merchantServer 402s the first request and serves `body` once the escrow
// header is present — the same handshake the mock merchant implements.
func merchantServer(t *testing.T, ch types.Challenge, status int, contentType, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(HdrEscrowID) == "" {
			WriteChallenge(w.Header(), ch)
			w.WriteHeader(http.StatusPaymentRequired)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testEngine(t *testing.T, chain types.Chain, led types.Ledger) *Engine {
	t.Helper()
	cfg := DefaultConfig()
	cfg.SweepGrace = 0
	return New(SingleChain(chain), led, Verifier{}, cfg)
}

func fetch(t *testing.T, e *Engine, url string) (*Result, error) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return e.Fetch(context.Background(), tenant, req)
}

func methods(calls []mocks.ChainCall) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.Method
	}
	return out
}

func ledgerMethods(calls []mocks.LedgerCall) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.Method
	}
	return out
}

func want(t *testing.T, got, expected []string, what string) {
	t.Helper()
	if len(got) != len(expected) {
		t.Fatalf("%s calls = %v, want %v", what, got, expected)
	}
	for i := range got {
		if got[i] != expected[i] {
			t.Fatalf("%s calls = %v, want %v", what, got, expected)
		}
	}
}

func TestFetchNoPaymentRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("free"))
	}))
	defer srv.Close()

	chain, led := &mocks.MockChain{}, &mocks.MockLedger{}
	res, err := fetch(t, testEngine(t, chain, led), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if res.Paid || string(res.Body) != "free" {
		t.Fatalf("unexpected result %+v", res)
	}
	want(t, methods(chain.Calls), nil, "chain")
}

func TestFetchDeliveredReleases(t *testing.T) {
	const body = `{"temp":21}`
	ch := types.Challenge{
		Price: price, Merchant: merchantA, ResourceID: "weather",
		ResourceHash: hashOf(body), ContentType: "application/json", MinBytes: 4,
	}
	srv := merchantServer(t, ch, 200, "application/json", body)

	chain, led := &mocks.MockChain{}, &mocks.MockLedger{}
	res, err := fetch(t, testEngine(t, chain, led), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict.Outcome != types.VerdictDelivered {
		t.Fatalf("verdict = %+v", res.Verdict)
	}
	if string(res.Body) != body {
		t.Fatalf("body = %q, want %q", res.Body, body)
	}
	want(t, methods(chain.Calls), []string{"Lock", "Release"}, "chain")
	want(t, ledgerMethods(led.Calls), []string{"Open", "MarkDelivered", "MarkSettled"}, "ledger")

	settled := led.Calls[2]
	if settled.Args[2] != types.StatusReleased {
		t.Fatalf("settled status = %v, want released", settled.Args[2])
	}
	if settled.Args[0] != tenant {
		t.Fatalf("settle not scoped to tenant: %v", settled.Args[0])
	}
}

func TestFetchAbsentLeavesEscrowToSweeper(t *testing.T) {
	ch := types.Challenge{
		Price: price, Merchant: merchantA,
		ResourceHash: hashOf(`{"temp":21}`), ContentType: "application/json", MinBytes: 4,
	}
	// 200 with an empty body: the fail-after-settlement shape.
	srv := merchantServer(t, ch, 200, "application/json", "")

	chain, led := &mocks.MockChain{}, &mocks.MockLedger{}
	e := testEngine(t, chain, led)
	res, err := fetch(t, e, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict.Outcome != types.VerdictAbsent || res.Verdict.Reason != ReasonEmptyBody {
		t.Fatalf("verdict = %+v", res.Verdict)
	}
	// Nothing settles: no Release, no Refund, no MarkSettled.
	want(t, methods(chain.Calls), []string{"Lock"}, "chain")
	want(t, ledgerMethods(led.Calls), []string{"Open"}, "ledger")
}

func TestFetchPartialReleasesShare(t *testing.T) {
	ch := types.Challenge{
		Price: price, Merchant: merchantA,
		ResourceHash: hashOf(`{"temp":21}`), ContentType: "application/json", MinBytes: 4,
	}
	srv := merchantServer(t, ch, 200, "application/json", `{"te`)

	chain, led := &mocks.MockChain{}, &mocks.MockLedger{}
	res, err := fetch(t, testEngine(t, chain, led), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict.Outcome != types.VerdictPartial {
		t.Fatalf("verdict = %+v", res.Verdict)
	}
	want(t, methods(chain.Calls), []string{"Lock", "ReleasePartial"}, "chain")

	paid := chain.Calls[1].Args[1].(*big.Int)
	half := new(big.Int).Div(price, big.NewInt(2))
	if paid.Cmp(half) != 0 {
		t.Fatalf("partial paid %s, want %s", paid, half)
	}
}

func TestFetchOpenCircuitFailsBeforeLocking(t *testing.T) {
	ch := types.Challenge{Price: price, Merchant: merchantA, ResourceHash: hashOf("x")}
	srv := merchantServer(t, ch, 200, "application/json", "")

	chain, led := &mocks.MockChain{}, &mocks.MockLedger{}
	e := testEngine(t, chain, led)

	// Three absent deliveries trip the default threshold.
	for i := 0; i < DefaultConfig().BreakerThreshold; i++ {
		if _, err := fetch(t, e, srv.URL); err != nil {
			t.Fatal(err)
		}
	}
	before := len(chain.Calls)

	_, err := fetch(t, e, srv.URL)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("err = %v, want ErrCircuitOpen", err)
	}
	if len(chain.Calls) != before {
		t.Fatalf("circuit opened but chain was still touched: %v", methods(chain.Calls))
	}
}

func TestFetchRejectsPriceAboveCap(t *testing.T) {
	ch := types.Challenge{Price: big.NewInt(1e18), Merchant: merchantA}
	srv := merchantServer(t, ch, 200, "application/json", "hi")

	chain, led := &mocks.MockChain{}, &mocks.MockLedger{}
	if _, err := fetch(t, testEngine(t, chain, led), srv.URL); err == nil {
		t.Fatal("want error for price above cap")
	}
	want(t, methods(chain.Calls), nil, "chain")
}

func TestSweepRefundsExpired(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	expired := types.Obligation{
		EscrowID: 7, TenantID: tenant, Merchant: merchantA, Amount: price,
		ReleaseDeadline: now.Add(-time.Minute), Status: types.StatusLocked,
	}

	chain := &mocks.MockChain{}
	led := &mocks.MockLedger{
		PendingFn: func(context.Context, types.TenantID) ([]types.Obligation, error) {
			return []types.Obligation{expired}, nil
		},
	}
	e := testEngine(t, chain, led)
	e.now = func() time.Time { return now }

	n, err := e.Sweep(context.Background(), tenant)
	if err != nil || n != 1 {
		t.Fatalf("swept %d, err %v", n, err)
	}
	want(t, methods(chain.Calls), []string{"Refund"}, "chain")

	settled := led.Calls[1]
	if settled.Method != "MarkSettled" || settled.Args[2] != types.StatusRefunded {
		t.Fatalf("unexpected ledger call %+v", settled)
	}
}

func TestSweepWaitsOutTheGracePeriod(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	ob := types.Obligation{
		EscrowID: 7, TenantID: tenant, Merchant: merchantA, Amount: price,
		ReleaseDeadline: now.Add(-5 * time.Second), Status: types.StatusLocked,
	}

	chain := &mocks.MockChain{}
	led := &mocks.MockLedger{
		PendingFn: func(context.Context, types.TenantID) ([]types.Obligation, error) {
			return []types.Obligation{ob}, nil
		},
	}
	cfg := DefaultConfig() // SweepGrace = 30s
	e := New(SingleChain(chain), led, Verifier{}, cfg)
	e.now = func() time.Time { return now }

	if n, err := e.Sweep(context.Background(), tenant); err != nil || n != 0 {
		t.Fatalf("swept %d, err %v — should have waited out the grace period", n, err)
	}
	want(t, methods(chain.Calls), nil, "chain")
}

func TestSweepIsRetriedAfterAFailedRefund(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	ob := types.Obligation{
		EscrowID: 7, TenantID: tenant, Merchant: merchantA, Amount: price,
		ReleaseDeadline: now.Add(-time.Minute), Status: types.StatusLocked,
	}

	fail := true
	chain := &mocks.MockChain{
		RefundFn: func(context.Context, types.EscrowID) (types.TxHash, error) {
			if fail {
				return "", errors.New("rpc down")
			}
			return "0xabc", nil
		},
	}
	led := &mocks.MockLedger{
		PendingFn: func(context.Context, types.TenantID) ([]types.Obligation, error) {
			return []types.Obligation{ob}, nil // still LOCKED, so it comes back
		},
	}
	e := testEngine(t, chain, led)
	e.now = func() time.Time { return now }

	if n, _ := e.Sweep(context.Background(), tenant); n != 0 {
		t.Fatalf("swept %d despite refund failure", n)
	}
	fail = false
	if n, err := e.Sweep(context.Background(), tenant); err != nil || n != 1 {
		t.Fatalf("retry swept %d, err %v", n, err)
	}
}

func TestParseChallengeRejectsGarbage(t *testing.T) {
	tests := []struct {
		name string
		hdr  map[string]string
	}{
		{"no price", map[string]string{HdrMerchant: merchantA.Hex()}},
		{"negative price", map[string]string{HdrPrice: "-1", HdrMerchant: merchantA.Hex()}},
		{"zero price", map[string]string{HdrPrice: "0", HdrMerchant: merchantA.Hex()}},
		{"not a number", map[string]string{HdrPrice: "1e18", HdrMerchant: merchantA.Hex()}},
		{"no merchant", map[string]string{HdrPrice: "1"}},
		{"zero merchant", map[string]string{HdrPrice: "1", HdrMerchant: (common.Address{}).Hex()}},
		{"short hash", map[string]string{HdrPrice: "1", HdrMerchant: merchantA.Hex(), HdrResourceHash: "0xdead"}},
		{"bad min bytes", map[string]string{HdrPrice: "1", HdrMerchant: merchantA.Hex(), HdrMinBytes: "-3"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tc.hdr {
				h.Set(k, v)
			}
			if _, err := ParseChallenge(h, nil); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestChallengeRoundTrip(t *testing.T) {
	in := types.Challenge{
		Price: price, Merchant: merchantA, ResourceID: "weather/nyc",
		ResourceHash: hashOf("body"), ContentType: "application/json", MinBytes: 12,
	}
	h := http.Header{}
	WriteChallenge(h, in)

	out, err := ParseChallenge(h, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Price.Cmp(in.Price) != 0 || out.Merchant != in.Merchant ||
		out.ResourceID != in.ResourceID || out.ResourceHash != in.ResourceHash ||
		out.ContentType != in.ContentType || out.MinBytes != in.MinBytes {
		t.Fatalf("round trip lost data:\n got %+v\nwant %+v", out, in)
	}
}
