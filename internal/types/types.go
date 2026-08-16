// Package types defines the shared vocabulary for the Leash sidecar.
// No logic, no I/O — types and interfaces only.
package types

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// ---------------------------------------------------------------------------
// Scalar aliases
// ---------------------------------------------------------------------------

// EscrowID is the on-chain identifier for a single escrow.
type EscrowID uint64

// TxHash is the hex-encoded transaction hash (with 0x prefix).
type TxHash string

// TenantID scopes every ledger row, breaker counter, and balance query
// to a single agent identity.
type TenantID string

// ---------------------------------------------------------------------------
// Enums
// ---------------------------------------------------------------------------

// Status represents the lifecycle state of an escrow obligation.
type Status string

const (
	StatusLocked   Status = "locked"
	StatusReleased Status = "released"
	StatusRefunded Status = "refunded"
	StatusPartial  Status = "partial"
)

// Verdict is the outcome of a delivery verification check.
type Verdict struct {
	// Outcome is the high-level result of the check.
	Outcome VerdictOutcome
	// Reason is a human-readable explanation of the verdict.
	Reason string
}

// VerdictOutcome enumerates the possible verification results.
type VerdictOutcome string

const (
	VerdictDelivered VerdictOutcome = "delivered"
	VerdictPartial   VerdictOutcome = "partial"
	VerdictAbsent    VerdictOutcome = "absent"
)

// ---------------------------------------------------------------------------
// Domain structs
// ---------------------------------------------------------------------------

// Obligation is the local bookkeeping record for a single locked escrow.
type Obligation struct {
	EscrowID        EscrowID
	TenantID        TenantID
	Merchant        common.Address
	Amount          *big.Int // wei — never float64, never int64
	ResourceID      string   // the merchant's label for what was bought
	ResourceHash    [32]byte
	LockedAt        time.Time
	ReleaseDeadline time.Time
	DeliveredAt     *time.Time // nil until delivery is confirmed
	Status          Status
	LockTx          TxHash // the tx that created the escrow
	SettleTx        TxHash
}

// Challenge describes the parameters the verifier uses to judge a response.
type Challenge struct {
	Price        *big.Int       // wei
	Merchant     common.Address
	ResourceID   string
	ResourceHash [32]byte
	ContentType  string
	MinBytes     int
}

// Balances is a point-in-time snapshot of a tenant's fund allocation.
type Balances struct {
	Spendable    *big.Int // wei available for new locks
	Locked       *big.Int // wei held in active escrows
	Recovered    *big.Int // wei returned via refunds/partial releases
	PendingCount int      // number of unresolved obligations
}

// EscrowEventKind enumerates the types of on-chain escrow events.
type EscrowEventKind string

const (
	EventLocked   EscrowEventKind = "locked"
	EventReleased EscrowEventKind = "released"
	EventRefunded EscrowEventKind = "refunded"
)

// EscrowEvent is a notification emitted by the chain subscription.
type EscrowEvent struct {
	Kind        EscrowEventKind
	EscrowID    EscrowID
	TxHash      TxHash
	BlockNumber uint64
}

// ---------------------------------------------------------------------------
// Interfaces
// ---------------------------------------------------------------------------

// Chain abstracts all on-chain interactions with the PaymentEscrow contract.
type Chain interface {
	// Lock creates a new escrow, sending `amount` wei to the contract.
	Lock(ctx context.Context, merchant common.Address, amount *big.Int, resourceHash [32]byte) (EscrowID, TxHash, error)

	// Release settles the full escrow amount to the merchant.
	Release(ctx context.Context, id EscrowID) (TxHash, error)

	// Refund returns the full escrow amount to the buyer.
	Refund(ctx context.Context, id EscrowID) (TxHash, error)

	// ReleasePartial settles a portion of the escrow to the merchant and
	// refunds the remainder to the buyer.
	ReleasePartial(ctx context.Context, id EscrowID, amount *big.Int) (TxHash, error)

	// Subscribe streams escrow events for the buyer's address.
	// The returned channel is closed when ctx is cancelled.
	Subscribe(ctx context.Context) (<-chan EscrowEvent, error)

	// BalanceOf returns the native MON balance of the given address.
	BalanceOf(ctx context.Context, addr common.Address) (*big.Int, error)
}

// Ledger abstracts the local obligation store (SQLite).
// Every method that touches state takes or derives a TenantID.
type Ledger interface {
	// Open records a newly locked escrow obligation.
	Open(ctx context.Context, tenant TenantID, ob Obligation) error

	// MarkDelivered stamps the obligation with a delivery timestamp.
	MarkDelivered(ctx context.Context, tenant TenantID, id EscrowID, at time.Time) error

	// MarkSettled records the settlement transaction hash and final status.
	MarkSettled(ctx context.Context, tenant TenantID, id EscrowID, status Status, tx TxHash) error

	// Pending returns all obligations that have not yet settled for the tenant.
	Pending(ctx context.Context, tenant TenantID) ([]Obligation, error)

	// Snapshot returns the current balance breakdown for the tenant.
	Snapshot(ctx context.Context, tenant TenantID) (Balances, error)
}

// Verifier judges whether an HTTP response satisfies the delivery contract
// described by the Challenge. Deterministic only — no LLM, no probabilistic
// scoring.
type Verifier interface {
	// Verify inspects an already-read response against the challenge
	// parameters. The caller does all the I/O: it reads and closes the body
	// and passes the status and Content-Type in as plain values, so the
	// response stays readable downstream. A transport failure (timeout,
	// context deadline) is passed as status 0 with a nil body.
	Verify(ctx context.Context, c Challenge, status int, contentType string, body []byte) (Verdict, error)
}
