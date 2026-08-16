// Package leash gates an HTTP handler on a confirmed on-chain escrow.
//
// A merchant wraps a handler; the middleware answers first contact with the
// Leash 402 challenge, and on the paid retry reads the escrow contract to
// confirm that funds are actually locked for this request, naming this
// merchant, before the handler ever runs.
//
// The whole integration:
//
//	leash.Connect(ctx, os.Getenv("MONAD_RPC"), common.HexToAddress(os.Getenv("LEASH_ESCROW")))
//	http.Handle("/data", leash.Paid(leash.Terms{
//		Price:    big.NewInt(5e16), // 0.05 MON, locked until delivery is verified
//		Merchant: merchantAddr,     // where the buyer's release() sends it
//	}, dataHandler))
//
// This side never calls release() or refund(). Settlement is the buyer's
// sidecar's job, and deliberately so: the party that pays is the party that
// decides whether it got what it paid for. All this middleware does is refuse
// to serve until the money is provably locked.
package leash

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/A1kartikey/leash/internal/chain/bindings"
	"github.com/A1kartikey/leash/internal/engine"
	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Escrow status as the contract defines it. A nonexistent escrow reads as
// Locked with a zero buyer, which is why every check below tests the merchant
// and the amount too, never the status alone.
const (
	StatusLocked   uint8 = 0
	StatusReleased uint8 = 1
	StatusRefunded uint8 = 2
	StatusPartial  uint8 = 3
)

// LookupWindow is how long Paid keeps re-reading the escrow before giving up
// and re-issuing the challenge. Monad executes asynchronously, so a lock that
// is mined is not instantly visible to a read — without this window an honest
// buyer would be told to pay twice.
var LookupWindow = 3 * time.Second

// Terms are what the merchant demands for one resource.
type Terms struct {
	// Price in wei. The escrow must hold at least this much.
	Price *big.Int

	// Merchant is the payout address. The escrow must name it, or the money
	// is not ours and the handler must not run.
	Merchant common.Address

	// ResourceID is an opaque label echoed in the challenge; it shows up in
	// the buyer's ledger and dashboard.
	ResourceID string

	// ResourceHash is keccak256 of the exact body that will be served, when
	// the merchant can commit to it up front. Declaring it binds the escrow
	// to this specific resource: the buyer's verifier settles on a hash match
	// and the escrow this middleware accepts must carry the same hash.
	ResourceHash [32]byte

	// ContentType and MinBytes are the content contract offered when no hash
	// can be declared — the terms the buyer's verifier falls back to.
	ContentType string
	MinBytes    int
}

// Escrow is the on-chain record, as read.
type Escrow struct {
	Buyer        common.Address
	Merchant     common.Address
	Amount       *big.Int
	ResourceHash [32]byte
	Status       uint8
}

// Reader reads escrow state. The middleware only ever reads: it holds no key
// and can move no funds.
type Reader interface {
	Escrow(ctx context.Context, id uint64) (Escrow, error)
}

var (
	mu     sync.RWMutex
	reader Reader
	logger = slog.Default()
)

// Connect points the package at an escrow contract over JSON-RPC. Call it once
// at startup. It is read-only: no key, no signer, nothing to leak.
func Connect(ctx context.Context, rpcURL string, escrow common.Address) error {
	if rpcURL == "" {
		return errors.New("leash: no RPC url")
	}
	if escrow == (common.Address{}) {
		return errors.New("leash: no escrow contract address")
	}

	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return fmt.Errorf("leash: dialing %s: %w", rpcURL, err)
	}
	c, err := bindings.NewPaymentEscrow(escrow, client)
	if err != nil {
		client.Close()
		return fmt.Errorf("leash: binding escrow at %s: %w", escrow, err)
	}

	UseReader(&rpcReader{contract: c})
	return nil
}

// UseReader installs a Reader directly — a stub in tests, or AllowAll in an
// offline demo.
func UseReader(r Reader) {
	mu.Lock()
	defer mu.Unlock()
	reader = r
}

// SetLogger replaces the logger used for refusals.
func SetLogger(l *slog.Logger) {
	mu.Lock()
	defer mu.Unlock()
	logger = l
}

func current() (Reader, *slog.Logger) {
	mu.RLock()
	defer mu.RUnlock()
	return reader, logger
}

// rpcReader is the real thing: one eth_call per request.
type rpcReader struct {
	contract *bindings.PaymentEscrow
}

func (r *rpcReader) Escrow(ctx context.Context, id uint64) (Escrow, error) {
	e, err := r.contract.Escrows(&bind.CallOpts{Context: ctx}, new(big.Int).SetUint64(id))
	if err != nil {
		return Escrow{}, fmt.Errorf("leash: reading escrow %d: %w", id, err)
	}
	return Escrow{
		Buyer:        e.Buyer,
		Merchant:     e.Merchant,
		Amount:       e.Amount,
		ResourceHash: e.ResourceHash,
		Status:       e.Status,
	}, nil
}

// AllowAll answers every lookup with an escrow that satisfies the given terms,
// without reading a chain. It exists for offline demos and nothing else — a
// merchant running this serves resources to anyone who invents an escrow id.
func AllowAll(t Terms) Reader {
	return allowAll{t: t}
}

type allowAll struct{ t Terms }

func (a allowAll) Escrow(_ context.Context, _ uint64) (Escrow, error) {
	return Escrow{
		Merchant:     a.t.Merchant,
		Amount:       a.t.Price,
		ResourceHash: a.t.ResourceHash,
		Status:       StatusLocked,
	}, nil
}

// ---------------------------------------------------------------------------
// The middleware
// ---------------------------------------------------------------------------

// Paid wraps next so it only runs once an escrow is confirmed locked on-chain
// for this request.
//
// First contact — no escrow header — is answered with the 402 challenge. On
// the retry the escrow is read and checked; anything short of a locked escrow
// naming this merchant for at least this price gets the challenge again,
// never the resource. Failing closed is the entire point: an escrow that
// cannot be confirmed is indistinguishable from one that was never created.
func Paid(t Terms, next http.Handler) http.Handler {
	if t.Price == nil || t.Price.Sign() <= 0 {
		panic("leash: Terms.Price must be a positive wei amount")
	}
	if t.Merchant == (common.Address{}) {
		panic("leash: Terms.Merchant must be set — that is where the money goes")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get(engine.HdrEscrowID)
		if raw == "" {
			challenge(w, t, "payment required")
			return
		}

		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			challenge(w, t, "bad escrow id")
			return
		}

		if err := confirm(r.Context(), t, id); err != nil {
			_, log := current()
			log.Warn("leash: refusing to serve", "escrow", id, "err", err)
			challenge(w, t, err.Error())
			return
		}

		w.Header().Set(hdrVerified, raw)
		next.ServeHTTP(w, r)
	})
}

// hdrVerified tells the buyer (and anyone reading a trace) that this response
// was gated on a confirmed lock rather than served on trust.
const hdrVerified = "X-Payment-Escrow-Verified"

// confirm reads the escrow and checks it against the terms, retrying inside
// LookupWindow so an asynchronously-executed lock has time to become visible.
func confirm(ctx context.Context, t Terms, id uint64) error {
	rd, _ := current()
	if rd == nil {
		return errors.New("no escrow reader configured")
	}

	deadline := time.Now().Add(LookupWindow)
	for {
		e, err := rd.Escrow(ctx, id)
		if err == nil {
			if err = Check(t, e); err == nil {
				return nil
			}
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// Check is the gate, as a pure function: no I/O, so the rules are readable
// and testable on their own.
//
//	the escrow names this merchant
//	AND it holds at least the asking price
//	AND it is still Locked
//	AND, when the terms commit to a body hash, it is that resource's escrow
func Check(t Terms, e Escrow) error {
	if e.Merchant != t.Merchant {
		return fmt.Errorf("escrow names %s, not %s", e.Merchant, t.Merchant)
	}
	if e.Amount == nil || e.Amount.Cmp(t.Price) < 0 {
		return fmt.Errorf("escrow holds %s wei, price is %s wei", e.Amount, t.Price)
	}
	if e.Status != StatusLocked {
		return fmt.Errorf("escrow status %d, want Locked(%d)", e.Status, StatusLocked)
	}
	if t.ResourceHash != ([32]byte{}) && e.ResourceHash != t.ResourceHash {
		return errors.New("escrow was locked for a different resource")
	}
	return nil
}

// challenge writes the 402 in Leash's dialect — the same writer the sidecar
// parses, so merchant and buyer can never drift into different formats.
func challenge(w http.ResponseWriter, t Terms, reason string) {
	engine.WriteChallenge(w.Header(), types.Challenge{
		Price:        t.Price,
		Merchant:     t.Merchant,
		ResourceID:   t.ResourceID,
		ResourceHash: t.ResourceHash,
		ContentType:  t.ContentType,
		MinBytes:     t.MinBytes,
	})
	w.Header().Set("X-Payment-Reason", reason)
	w.WriteHeader(http.StatusPaymentRequired)
}
