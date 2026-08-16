// Package engine implements the Leash settlement path: 402 handling, escrow
// lifecycle, deterministic verification, the refund sweeper, and the circuit
// breaker.
//
// The invariant everything here exists to enforce: funds move to a merchant
// only after this package has independently verified that a valid response was
// delivered.
//
// Every function that touches state takes a TenantID. A tenant's signer is
// reachable only through its own chain handle — keys never cross tenants.
package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strconv"
	"time"

	"github.com/A1kartikey/leash/internal/types"
)

// Config tunes the settlement path.
type Config struct {
	// TTL must match the chain wrapper's DefaultTTL: it is what the local
	// release_deadline is computed from, and the sweeper refunds against it.
	TTL time.Duration

	// SweepInterval is how often the single sweeper goroutine polls Pending().
	SweepInterval time.Duration

	// SweepGrace delays a refund past the local deadline. The contract compares
	// against block.timestamp, which drifts from our clock; refunding early
	// reverts with TooEarly. Tune this if the chain's clock runs behind.
	SweepGrace time.Duration

	// PartialBps is the share (basis points) paid to the merchant on a Partial
	// verdict. The remainder is refunded in the same transaction.
	PartialBps int64

	// MaxPrice caps what a merchant may demand in one challenge. Zero or nil
	// disables the cap. This is a trust boundary: the merchant sets the price.
	MaxPrice *big.Int

	BreakerThreshold int
	BreakerCooldown  time.Duration
}

func DefaultConfig() Config {
	return Config{
		TTL:              1 * time.Hour,
		SweepInterval:    15 * time.Second,
		SweepGrace:       30 * time.Second,
		PartialBps:       5000, // ponytail: flat 50%; per-challenge terms if merchants ever negotiate
		MaxPrice:         new(big.Int).SetUint64(100_000_000_000_000_000), // 0.1 MON
		BreakerThreshold: 3,
		BreakerCooldown:  60 * time.Second,
	}
}

// ChainFor resolves a tenant to its own chain handle, which owns that tenant's
// signer. There is deliberately no way to reach a signer without a TenantID.
type ChainFor func(types.TenantID) (types.Chain, error)

// SingleChain is a ChainFor for single-tenant deployments and tests.
func SingleChain(c types.Chain) ChainFor {
	return func(types.TenantID) (types.Chain, error) { return c, nil }
}

// Engine drives the paid-request lifecycle.
type Engine struct {
	chainFor ChainFor
	ledger   types.Ledger
	verifier types.Verifier
	breaker  *Breaker
	http     *http.Client
	cfg      Config
	log      *slog.Logger
	now      func() time.Time
}

func New(chainFor ChainFor, led types.Ledger, ver types.Verifier, cfg Config) *Engine {
	return &Engine{
		chainFor: chainFor,
		ledger:   led,
		verifier: ver,
		breaker:  NewBreaker(cfg.BreakerThreshold, cfg.BreakerCooldown),
		http:     &http.Client{Timeout: 30 * time.Second},
		cfg:      cfg,
		log:      slog.Default(),
		now:      time.Now,
	}
}

// Breaker exposes the circuit breaker for status display.
func (e *Engine) Breaker() *Breaker { return e.breaker }

// Result is the outcome of one Fetch.
type Result struct {
	Status  int
	Header  http.Header
	Body    []byte
	Paid    bool
	Verdict types.Verdict // zero unless Paid
	// Challenge is the terms the merchant demanded; nil unless Paid.
	Challenge *types.Challenge
	EscrowID  types.EscrowID
	LockTx    types.TxHash
	SettleTx  types.TxHash
}

// Fetch performs the full lifecycle for one request on behalf of tenant:
//
//	402 -> parse Challenge -> circuit check -> Lock -> ledger.Open
//	-> retry -> Verify
//	   Delivered -> Release        -> MarkSettled
//	   Partial   -> ReleasePartial -> MarkSettled
//	   Absent    -> no action; the sweeper refunds after the deadline
//
// A non-402 response is returned untouched and costs nothing.
// ErrCircuitOpen is returned before any funds are locked; map it to 503.
func (e *Engine) Fetch(ctx context.Context, tenant types.TenantID, req *http.Request) (*Result, error) {
	reqBody, err := drain(req)
	if err != nil {
		return nil, fmt.Errorf("engine: reading request body: %w", err)
	}

	resp, err := e.send(ctx, req, reqBody, nil, "")
	if err != nil {
		return nil, fmt.Errorf("engine: initial request: %w", err)
	}
	if resp.StatusCode != http.StatusPaymentRequired {
		return readResult(resp)
	}

	ch, err := ParseChallenge(resp.Header, e.cfg.MaxPrice)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}

	// Circuit check happens BEFORE any funds are locked.
	if err := e.breaker.Allow(tenant, ch.Merchant); err != nil {
		return nil, err
	}

	chain, err := e.chainFor(tenant)
	if err != nil {
		return nil, fmt.Errorf("engine: chain for tenant %s: %w", tenant, err)
	}

	id, lockTx, err := chain.Lock(ctx, ch.Merchant, ch.Price, ch.ResourceHash)
	if err != nil {
		return nil, fmt.Errorf("engine: lock: %w", err)
	}

	now := e.now()
	ob := types.Obligation{
		EscrowID:        id,
		TenantID:        tenant,
		Merchant:        ch.Merchant,
		Amount:          ch.Price,
		ResourceID:      ch.ResourceID,
		ResourceHash:    ch.ResourceHash,
		LockedAt:        now,
		ReleaseDeadline: now.Add(e.cfg.TTL),
		Status:          types.StatusLocked,
		LockTx:          lockTx,
	}
	if err := e.ledger.Open(ctx, tenant, ob); err != nil {
		// Funds are locked but unrecorded. Refuse to settle blind: the escrow
		// is still on-chain and the buyer can refund it after the deadline.
		// ponytail: recovery by EscrowLocked event replay when cmd/leash lands.
		return nil, fmt.Errorf("engine: ledger open escrow %d: %w", id, err)
	}

	res := &Result{Paid: true, Challenge: &ch, EscrowID: id, LockTx: lockTx}

	resp, err = e.send(ctx, req, reqBody, &id, lockTx)
	if err != nil {
		// No response at all (timeout, context deadline) is an absent delivery,
		// not an engine failure: status 0, nil body.
		e.breaker.Failure(tenant, ch.Merchant)
		res.Verdict, _ = e.verifier.Verify(ctx, ch, 0, "", nil)
		res.Status = http.StatusBadGateway
		return res, fmt.Errorf("engine: paid request: %w", err)
	}

	// Read the body once, here. Verify() is pure and never touches the wire,
	// so the response stays readable: it is handed back with a fresh reader
	// over the same bytes for anything downstream that forwards or logs it.
	body, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))

	verdict, err := e.verifier.Verify(ctx, ch, resp.StatusCode, resp.Header.Get("Content-Type"), body)
	if err != nil {
		return nil, fmt.Errorf("engine: verify escrow %d: %w", id, err)
	}
	res.Verdict = verdict
	res.Status, res.Header, res.Body = resp.StatusCode, resp.Header, body

	if err := e.settle(ctx, chain, tenant, ch, res); err != nil {
		return res, err
	}
	return res, nil
}

// settle applies the verdict on-chain and in the ledger. Absent deliberately
// does nothing: the sweeper refunds after the deadline, which is also the
// restart-recovery path.
func (e *Engine) settle(ctx context.Context, chain types.Chain, tenant types.TenantID, ch types.Challenge, res *Result) error {
	switch res.Verdict.Outcome {
	case types.VerdictDelivered:
		if err := e.ledger.MarkDelivered(ctx, tenant, res.EscrowID, e.now()); err != nil {
			return fmt.Errorf("engine: mark delivered %d: %w", res.EscrowID, err)
		}
		tx, err := chain.Release(ctx, res.EscrowID)
		if err != nil {
			return fmt.Errorf("engine: release %d: %w", res.EscrowID, err)
		}
		res.SettleTx = tx
		e.breaker.Success(tenant, ch.Merchant)
		return e.ledger.MarkSettled(ctx, tenant, res.EscrowID, types.StatusReleased, tx)

	case types.VerdictPartial:
		if err := e.ledger.MarkDelivered(ctx, tenant, res.EscrowID, e.now()); err != nil {
			return fmt.Errorf("engine: mark delivered %d: %w", res.EscrowID, err)
		}
		amount := new(big.Int).Mul(ch.Price, big.NewInt(e.cfg.PartialBps))
		amount.Div(amount, big.NewInt(10_000))
		tx, err := chain.ReleasePartial(ctx, res.EscrowID, amount)
		if err != nil {
			return fmt.Errorf("engine: release partial %d: %w", res.EscrowID, err)
		}
		res.SettleTx = tx
		// Partial is not a delivery: it counts against the merchant.
		e.breaker.Failure(tenant, ch.Merchant)
		return e.ledger.MarkSettled(ctx, tenant, res.EscrowID, types.StatusPartial, tx)

	default: // VerdictAbsent
		e.breaker.Failure(tenant, ch.Merchant)
		e.log.Warn("delivery absent; leaving escrow to the sweeper",
			"tenant", tenant, "escrow", res.EscrowID, "reason", res.Verdict.Reason)
		return nil
	}
}

// ---------------------------------------------------------------------------
// Sweeper
// ---------------------------------------------------------------------------

// Sweep refunds every expired obligation for one tenant. It is idempotent:
// a row stays LOCKED until its refund lands, so a failed tick simply retries
// on the next one. Restart recovery needs no special code path.
func (e *Engine) Sweep(ctx context.Context, tenant types.TenantID) (int, error) {
	pending, err := e.ledger.Pending(ctx, tenant)
	if err != nil {
		return 0, fmt.Errorf("engine: sweep pending %s: %w", tenant, err)
	}
	if len(pending) == 0 {
		return 0, nil
	}

	chain, err := e.chainFor(tenant)
	if err != nil {
		return 0, fmt.Errorf("engine: chain for tenant %s: %w", tenant, err)
	}

	n := 0
	for _, ob := range pending {
		if e.now().Before(ob.ReleaseDeadline.Add(e.cfg.SweepGrace)) {
			continue // our clock is ahead of the chain's; wait it out
		}
		tx, err := chain.Refund(ctx, ob.EscrowID)
		if err != nil {
			e.log.Warn("refund failed; retrying next tick",
				"tenant", tenant, "escrow", ob.EscrowID, "err", err)
			continue
		}
		if err := e.ledger.MarkSettled(ctx, tenant, ob.EscrowID, types.StatusRefunded, tx); err != nil {
			e.log.Error("refunded on-chain but ledger not updated",
				"tenant", tenant, "escrow", ob.EscrowID, "tx", tx, "err", err)
			continue
		}
		n++
	}
	return n, nil
}

// RunSweeper is the single sweeper goroutine. One goroutine for the whole
// process — never a time.AfterFunc per escrow, which leaks under load and dies
// with the process, defeating the durability story.
//
// tenants is called each tick so tenants added at runtime are picked up.
// Blocks until ctx is cancelled.
func (e *Engine) RunSweeper(ctx context.Context, tenants func() []types.TenantID) {
	t := time.NewTicker(e.cfg.SweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, tenant := range tenants() {
				n, err := e.Sweep(ctx, tenant)
				if err != nil && !errors.Is(err, context.Canceled) {
					e.log.Error("sweep failed", "tenant", tenant, "err", err)
				}
				if n > 0 {
					e.log.Info("swept expired escrows", "tenant", tenant, "refunded", n)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// HTTP plumbing
// ---------------------------------------------------------------------------

// drain buffers the request body so the retry can replay it.
func drain(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	b, err := io.ReadAll(req.Body)
	req.Body.Close()
	req.Body = nil
	return b, err
}

func (e *Engine) send(ctx context.Context, req *http.Request, body []byte, id *types.EscrowID, tx types.TxHash) (*http.Response, error) {
	r := req.Clone(ctx)
	if body != nil {
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		r.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
	}
	if id != nil {
		r.Header.Set(HdrEscrowID, strconv.FormatUint(uint64(*id), 10))
		r.Header.Set(HdrLockTx, string(tx))
	}
	return e.http.Do(r)
}

// MaxBodyBytes caps how much of a merchant response is read. A merchant is a
// trust boundary: an unbounded body is an OOM they control.
const MaxBodyBytes = 8 << 20 // 8 MiB

// readBody reads and closes the response body, capped at MaxBodyBytes.
func readBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("engine: reading response body: %w", err)
	}
	return b, nil
}

func readResult(resp *http.Response) (*Result, error) {
	b, err := readBody(resp)
	if err != nil {
		return nil, err
	}
	return &Result{Status: resp.StatusCode, Header: resp.Header, Body: b}, nil
}
