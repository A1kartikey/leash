package leash

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/A1kartikey/leash/internal/engine"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	merchant = common.HexToAddress("0x000000000000000000000000000000000000dEaD")
	stranger = common.HexToAddress("0x00000000000000000000000000000000BadBad00")
	price    = big.NewInt(50_000_000_000_000_000) // 0.05 MON
)

func terms() Terms {
	return Terms{Price: price, Merchant: merchant, ResourceID: "weather/nyc"}
}

func locked() Escrow {
	return Escrow{Buyer: stranger, Merchant: merchant, Amount: price, Status: StatusLocked}
}

// stubReader stands in for the chain.
type stubReader struct {
	e     Escrow
	err   error
	calls int
}

func (s *stubReader) Escrow(context.Context, uint64) (Escrow, error) {
	s.calls++
	return s.e, s.err
}

// Check is the gate. Everything short of "locked, mine, and enough" must fail.
func TestCheck(t *testing.T) {
	otherHash := crypto.Keccak256Hash([]byte("some other resource"))

	tests := []struct {
		name  string
		terms Terms
		e     Escrow
		ok    bool
	}{
		{"locked escrow for this merchant", terms(), locked(), true},
		{"overpaid is fine", terms(), Escrow{Merchant: merchant, Amount: big.NewInt(6e16), Status: StatusLocked}, true},

		{"names another merchant", terms(), Escrow{Merchant: stranger, Amount: price, Status: StatusLocked}, false},
		{"underpaid", terms(), Escrow{Merchant: merchant, Amount: big.NewInt(4e16), Status: StatusLocked}, false},
		{"already released", terms(), Escrow{Merchant: merchant, Amount: price, Status: StatusReleased}, false},
		{"already refunded", terms(), Escrow{Merchant: merchant, Amount: price, Status: StatusRefunded}, false},
		{"partially settled", terms(), Escrow{Merchant: merchant, Amount: price, Status: StatusPartial}, false},

		// A nonexistent escrow reads as a zero struct: Locked, but with no
		// merchant and no money. It must never pass.
		{"nonexistent escrow", terms(), Escrow{Amount: new(big.Int)}, false},
		{"nil amount", terms(), Escrow{Merchant: merchant, Status: StatusLocked}, false},

		{
			"hash-bound terms, matching escrow",
			Terms{Price: price, Merchant: merchant, ResourceHash: otherHash},
			Escrow{Merchant: merchant, Amount: price, Status: StatusLocked, ResourceHash: otherHash},
			true,
		},
		{
			"hash-bound terms, escrow for another resource",
			Terms{Price: price, Merchant: merchant, ResourceHash: otherHash},
			locked(),
			false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Check(tc.terms, tc.e)
			if tc.ok && err != nil {
				t.Fatalf("Check rejected a valid escrow: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("Check accepted an escrow it must refuse")
			}
		})
	}
}

// served reports whether the wrapped handler ran, and the response code.
func served(t *testing.T, r Reader, escrowHeader string) (bool, int) {
	t.Helper()

	UseReader(r)
	ran := false
	h := Paid(terms(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ran = true
		w.Write([]byte("the paid-for resource"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/data", nil)
	if escrowHeader != "" {
		req.Header.Set(engine.HdrEscrowID, escrowHeader)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return ran, rec.Code
}

// The handler runs only once the money is confirmed locked. Everything else
// gets the challenge back — never the resource.
func TestPaidGatesOnConfirmedLock(t *testing.T) {
	t.Cleanup(func() { UseReader(nil) })
	old := LookupWindow
	LookupWindow = 0 // no waiting for a stub that will not change its mind
	t.Cleanup(func() { LookupWindow = old })

	tests := []struct {
		name   string
		reader Reader
		header string
		serve  bool
	}{
		{"first contact, no escrow header", &stubReader{e: locked()}, "", false},
		{"confirmed lock", &stubReader{e: locked()}, "14", true},
		{"escrow id is not a number", &stubReader{e: locked()}, "fourteen", false},
		{"escrow names another merchant", &stubReader{e: Escrow{Merchant: stranger, Amount: price}}, "14", false},
		{"escrow already released", &stubReader{e: Escrow{Merchant: merchant, Amount: price, Status: StatusReleased}}, "14", false},
		{"escrow does not exist", &stubReader{e: Escrow{Amount: new(big.Int)}}, "14", false},
		{"chain unreachable", &stubReader{err: errors.New("rpc down")}, "14", false},
		{"no reader configured", nil, "14", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ran, code := served(t, tc.reader, tc.header)
			if ran != tc.serve {
				t.Fatalf("handler ran = %v, want %v (status %d)", ran, tc.serve, code)
			}
			want := http.StatusPaymentRequired
			if tc.serve {
				want = http.StatusOK
			}
			if code != want {
				t.Fatalf("status %d, want %d", code, want)
			}
		})
	}
}

// The 402 must carry terms the buyer's sidecar can actually parse — the
// merchant and the sidecar share one dialect or the handshake never starts.
func TestChallengeIsParseableByTheSidecar(t *testing.T) {
	t.Cleanup(func() { UseReader(nil) })
	UseReader(&stubReader{e: locked()})

	h := Paid(Terms{
		Price:        price,
		Merchant:     merchant,
		ResourceID:   "weather/nyc",
		ResourceHash: crypto.Keccak256Hash([]byte("body")),
		ContentType:  "application/json",
		MinBytes:     16,
	}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/data", nil))

	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("status %d, want 402", rec.Code)
	}
	ch, err := engine.ParseChallenge(rec.Header(), nil)
	if err != nil {
		t.Fatalf("the sidecar cannot parse our challenge: %v", err)
	}
	if ch.Price.Cmp(price) != 0 || ch.Merchant != merchant {
		t.Fatalf("challenge round-tripped wrong: %+v", ch)
	}
	if ch.ResourceID != "weather/nyc" || ch.ContentType != "application/json" || ch.MinBytes != 16 {
		t.Fatalf("challenge lost its terms: %+v", ch)
	}
}

// A lock that is mined but not yet visible must not cost the buyer a second
// payment: the middleware re-reads inside LookupWindow.
func TestPaidWaitsForALaggingRead(t *testing.T) {
	t.Cleanup(func() { UseReader(nil) })

	// Absent for the first read, locked afterwards — an asynchronously
	// executed chain catching up.
	appears := &lateReader{at: time.Now().Add(400 * time.Millisecond)}
	UseReader(appears)

	old := LookupWindow
	LookupWindow = 3 * time.Second
	t.Cleanup(func() { LookupWindow = old })

	ran := false
	h := Paid(terms(), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { ran = true }))

	req := httptest.NewRequest(http.MethodGet, "/data", nil)
	req.Header.Set(engine.HdrEscrowID, "14")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !ran {
		t.Fatalf("gave up on a lock that appeared %d reads in (status %d)", appears.calls, rec.Code)
	}
}

type lateReader struct {
	at    time.Time
	calls int
}

func (l *lateReader) Escrow(context.Context, uint64) (Escrow, error) {
	l.calls++
	if time.Now().Before(l.at) {
		return Escrow{Amount: new(big.Int)}, nil // reads as nonexistent
	}
	return locked(), nil
}
