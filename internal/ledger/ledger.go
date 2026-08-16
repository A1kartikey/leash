// Package ledger implements types.Ledger backed by SQLite.
//
// The PRIMARY KEY on escrow_id IS the deduplication guarantee — a duplicate
// insert is a no-op returning success, not an error. This replaces the
// in-memory cache whose failure is the exact vulnerability class this product
// exists to address.
//
// Wei is TEXT in SQLite and *big.Int in Go. Never REAL, never INTEGER, never
// float.
//
// Every method is scoped by tenant_id. A query that reads or writes state
// without a tenant scope is a bug.
package ledger

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/A1kartikey/leash/internal/types"
	"github.com/ethereum/go-ethereum/common"

	// Pure-Go SQLite driver — no CGo required.
	_ "modernc.org/sqlite"
)

// schema is applied once on Open via IF NOT EXISTS so the call is idempotent
// across restarts.
const schema = `
CREATE TABLE IF NOT EXISTS obligations (
	escrow_id        INTEGER PRIMARY KEY,
	tenant_id        TEXT NOT NULL,
	merchant         TEXT NOT NULL,
	amount           TEXT NOT NULL,
	resource_id      TEXT NOT NULL DEFAULT '',
	resource_hash    BLOB NOT NULL,
	locked_at        INTEGER NOT NULL,
	release_deadline INTEGER NOT NULL,
	delivered_at     INTEGER,
	status           TEXT NOT NULL,
	lock_tx          TEXT,
	settle_tx        TEXT
);
CREATE INDEX IF NOT EXISTS ix_pending ON obligations(status, release_deadline);
CREATE INDEX IF NOT EXISTS ix_tenant  ON obligations(tenant_id, locked_at);
`

// SQLiteLedger implements types.Ledger using a local SQLite database.
type SQLiteLedger struct {
	db *sql.DB
}

// Compile-time check.
var _ types.Ledger = (*SQLiteLedger)(nil)

// New opens (or creates) the SQLite database at path and ensures the schema
// exists. The call is idempotent.
func New(path string) (*SQLiteLedger, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("ledger: opening %s: %w", path, err)
	}

	// SQLite allows exactly one writer. database/sql opens a pool, so two
	// tenants settling at the same instant become two writers and the second
	// gets SQLITE_BUSY immediately — which is not a slow query but an escrow
	// locked on-chain with no row recording it, the precise failure this
	// ledger exists to prevent.
	//
	// One connection makes the pool serialise writers in-process; busy_timeout
	// covers the case where another process holds the file. Writes here are
	// single small rows, so serialising them costs nothing worth measuring.
	//
	// ponytail: a single connection also serialises reads; give readers their
	// own pool if snapshot queries ever show up in a profile.
	db.SetMaxOpenConns(1)

	// WAL lets readers run while a writer holds the file, and unlike
	// busy_timeout it is stored in the database itself rather than per
	// connection.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("ledger: enabling WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("ledger: setting busy timeout: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("ledger: enabling foreign keys: %w", err)
	}

	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("ledger: applying schema: %w", err)
	}

	// Columns added after the first databases were written. ALTER is the whole
	// migration story here: the error on an already-migrated database is the
	// only signal we need.
	for _, alter := range []string{
		`ALTER TABLE obligations ADD COLUMN resource_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE obligations ADD COLUMN lock_tx TEXT`,
	} {
		if _, err := db.Exec(alter); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			db.Close()
			return nil, fmt.Errorf("ledger: migrating: %w", err)
		}
	}

	return &SQLiteLedger{db: db}, nil
}

// Close closes the underlying database connection.
func (l *SQLiteLedger) Close() error {
	return l.db.Close()
}

// ---------------------------------------------------------------------------
// types.Ledger implementation
// ---------------------------------------------------------------------------

// Open records a newly locked escrow obligation.
//
// The PRIMARY KEY on escrow_id guarantees deduplication: INSERT OR IGNORE
// makes a duplicate insert a silent no-op, not an error. This is critical —
// it replaces the in-memory cache whose failure is the vulnerability class
// Leash exists to address.
func (l *SQLiteLedger) Open(ctx context.Context, tenant types.TenantID, ob types.Obligation) error {
	const q = `
		INSERT OR IGNORE INTO obligations
			(escrow_id, tenant_id, merchant, amount, resource_id, resource_hash,
			 locked_at, release_deadline, delivered_at, status, lock_tx, settle_tx)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	var deliveredAt *int64
	if ob.DeliveredAt != nil {
		v := ob.DeliveredAt.Unix()
		deliveredAt = &v
	}

	var lockTx *string
	if ob.LockTx != "" {
		s := string(ob.LockTx)
		lockTx = &s
	}

	var settleTx *string
	if ob.SettleTx != "" {
		s := string(ob.SettleTx)
		settleTx = &s
	}

	_, err := l.db.ExecContext(ctx, q,
		int64(ob.EscrowID),
		string(tenant),
		ob.Merchant.Hex(),
		ob.Amount.String(), // wei as decimal TEXT — never REAL, never INTEGER
		ob.ResourceID,
		ob.ResourceHash[:],
		ob.LockedAt.Unix(),
		ob.ReleaseDeadline.Unix(),
		deliveredAt,
		string(ob.Status),
		lockTx,
		settleTx,
	)
	if err != nil {
		return fmt.Errorf("ledger: open escrow %d: %w", ob.EscrowID, err)
	}
	return nil
}

// MarkDelivered stamps the obligation with a delivery timestamp.
// Scoped by tenant_id — touching another tenant's data is a bug.
func (l *SQLiteLedger) MarkDelivered(ctx context.Context, tenant types.TenantID, id types.EscrowID, at time.Time) error {
	const q = `
		UPDATE obligations
		SET delivered_at = ?
		WHERE escrow_id = ? AND tenant_id = ?
	`
	res, err := l.db.ExecContext(ctx, q, at.Unix(), int64(id), string(tenant))
	if err != nil {
		return fmt.Errorf("ledger: mark delivered %d: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("ledger: escrow %d not found for tenant %s", id, tenant)
	}
	return nil
}

// MarkSettled records the settlement transaction hash and final status.
// Scoped by tenant_id.
func (l *SQLiteLedger) MarkSettled(ctx context.Context, tenant types.TenantID, id types.EscrowID, status types.Status, tx types.TxHash) error {
	const q = `
		UPDATE obligations
		SET status = ?, settle_tx = ?
		WHERE escrow_id = ? AND tenant_id = ?
	`
	res, err := l.db.ExecContext(ctx, q, string(status), string(tx), int64(id), string(tenant))
	if err != nil {
		return fmt.Errorf("ledger: mark settled %d: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("ledger: escrow %d not found for tenant %s", id, tenant)
	}
	return nil
}

// Pending returns obligations still LOCKED whose release_deadline has passed,
// ordered oldest first. This drives the sweeper.
//
// Uses ix_pending (status, release_deadline) for efficient lookup.
// Scoped by tenant_id.
func (l *SQLiteLedger) Pending(ctx context.Context, tenant types.TenantID) ([]types.Obligation, error) {
	const q = `
		SELECT escrow_id, tenant_id, merchant, amount, resource_id, resource_hash,
		       locked_at, release_deadline, delivered_at, status, lock_tx, settle_tx
		FROM obligations
		WHERE tenant_id = ? AND status = 'locked' AND release_deadline <= ?
		ORDER BY release_deadline ASC
	`

	now := time.Now().Unix()
	rows, err := l.db.QueryContext(ctx, q, string(tenant), now)
	if err != nil {
		return nil, fmt.Errorf("ledger: pending query: %w", err)
	}
	defer rows.Close()

	return scanObligations(rows)
}

// Get returns one obligation. Scoped by tenant_id: an escrow that belongs to
// another tenant reads as not found.
func (l *SQLiteLedger) Get(ctx context.Context, tenant types.TenantID, id types.EscrowID) (types.Obligation, error) {
	const q = `
		SELECT escrow_id, tenant_id, merchant, amount, resource_id, resource_hash,
		       locked_at, release_deadline, delivered_at, status, lock_tx, settle_tx
		FROM obligations
		WHERE tenant_id = ? AND escrow_id = ?
	`

	rows, err := l.db.QueryContext(ctx, q, string(tenant), int64(id))
	if err != nil {
		return types.Obligation{}, fmt.Errorf("ledger: get escrow %d: %w", id, err)
	}
	defer rows.Close()

	obs, err := scanObligations(rows)
	if err != nil {
		return types.Obligation{}, err
	}
	if len(obs) == 0 {
		return types.Obligation{}, fmt.Errorf("ledger: escrow %d not found for tenant %s", id, tenant)
	}
	return obs[0], nil
}

// Recent returns the tenant's most recent obligations, newest first. It seeds
// the dashboard feed on page load so a refresh does not start blank — the same
// rows the live event stream then appends to.
func (l *SQLiteLedger) Recent(ctx context.Context, tenant types.TenantID, limit int) ([]types.Obligation, error) {
	const q = `
		SELECT escrow_id, tenant_id, merchant, amount, resource_id, resource_hash,
		       locked_at, release_deadline, delivered_at, status, lock_tx, settle_tx
		FROM obligations
		WHERE tenant_id = ?
		ORDER BY locked_at DESC, escrow_id DESC
		LIMIT ?
	`

	rows, err := l.db.QueryContext(ctx, q, string(tenant), limit)
	if err != nil {
		return nil, fmt.Errorf("ledger: recent for %s: %w", tenant, err)
	}
	defer rows.Close()

	return scanObligations(rows)
}

// Merchants lists every merchant this tenant has locked funds to, newest
// first. It is what the dashboard iterates for balances and circuit state —
// the ledger is the only record of who we have paid.
func (l *SQLiteLedger) Merchants(ctx context.Context, tenant types.TenantID) ([]common.Address, error) {
	const q = `
		SELECT merchant, MAX(locked_at) AS last_seen
		FROM obligations
		WHERE tenant_id = ?
		GROUP BY merchant
		ORDER BY last_seen DESC
	`

	rows, err := l.db.QueryContext(ctx, q, string(tenant))
	if err != nil {
		return nil, fmt.Errorf("ledger: merchants for %s: %w", tenant, err)
	}
	defer rows.Close()

	var out []common.Address
	for rows.Next() {
		var hexAddr string
		var lastSeen int64
		if err := rows.Scan(&hexAddr, &lastSeen); err != nil {
			return nil, fmt.Errorf("ledger: merchants scan: %w", err)
		}
		out = append(out, common.HexToAddress(hexAddr))
	}
	return out, rows.Err()
}

// Snapshot returns Balances for the tenant:
//   - Locked  = sum of LOCKED amounts
//   - Recovered = sum of REFUNDED amounts
//   - PendingCount = count of LOCKED
//
// Scoped by tenant_id.
//
// We do NOT use SQL SUM() because SQLite coerces TEXT to REAL during
// aggregation, destroying precision for large wei values. Instead we
// fetch (amount, status) pairs and sum in Go with *big.Int.
func (l *SQLiteLedger) Snapshot(ctx context.Context, tenant types.TenantID) (types.Balances, error) {
	const q = `
		SELECT amount, status
		FROM obligations
		WHERE tenant_id = ?
	`

	rows, err := l.db.QueryContext(ctx, q, string(tenant))
	if err != nil {
		return types.Balances{}, fmt.Errorf("ledger: snapshot query: %w", err)
	}
	defer rows.Close()

	locked := new(big.Int)
	recovered := new(big.Int)
	pendingCount := 0

	for rows.Next() {
		var amountStr, status string
		if err := rows.Scan(&amountStr, &status); err != nil {
			return types.Balances{}, fmt.Errorf("ledger: snapshot scan: %w", err)
		}

		amt, ok := new(big.Int).SetString(amountStr, 10)
		if !ok {
			return types.Balances{}, fmt.Errorf("ledger: snapshot: invalid amount %q", amountStr)
		}

		switch types.Status(status) {
		case types.StatusLocked:
			locked.Add(locked, amt)
			pendingCount++
		case types.StatusRefunded:
			recovered.Add(recovered, amt)
		}
	}
	if err := rows.Err(); err != nil {
		return types.Balances{}, fmt.Errorf("ledger: snapshot iteration: %w", err)
	}

	return types.Balances{
		Locked:       locked,
		Recovered:    recovered,
		PendingCount: pendingCount,
		// Spendable is computed at a higher layer that knows the on-chain
		// balance; the ledger only tracks local bookkeeping.
	}, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// scanObligations reads obligation rows from a query result set.
func scanObligations(rows *sql.Rows) ([]types.Obligation, error) {
	var out []types.Obligation
	for rows.Next() {
		var (
			escrowID        int64
			tenantID        string
			merchantHex     string
			amountStr       string
			resourceID      string
			resourceHash    []byte
			lockedAt        int64
			releaseDeadline int64
			deliveredAt     *int64
			status          string
			lockTx          *string
			settleTx        *string
		)

		if err := rows.Scan(
			&escrowID, &tenantID, &merchantHex, &amountStr, &resourceID, &resourceHash,
			&lockedAt, &releaseDeadline, &deliveredAt, &status, &lockTx, &settleTx,
		); err != nil {
			return nil, fmt.Errorf("ledger: scanning row: %w", err)
		}

		amount, ok := new(big.Int).SetString(amountStr, 10)
		if !ok {
			return nil, fmt.Errorf("ledger: invalid amount %q for escrow %d", amountStr, escrowID)
		}

		ob := types.Obligation{
			EscrowID:        types.EscrowID(escrowID),
			TenantID:        types.TenantID(tenantID),
			Merchant:        common.HexToAddress(merchantHex),
			Amount:          amount,
			ResourceID:      resourceID,
			LockedAt:        time.Unix(lockedAt, 0),
			ReleaseDeadline: time.Unix(releaseDeadline, 0),
			Status:          types.Status(status),
		}

		// ResourceHash: copy from BLOB into [32]byte.
		if len(resourceHash) == 32 {
			copy(ob.ResourceHash[:], resourceHash)
		} else if len(resourceHash) > 0 {
			// Handle hex-encoded hashes gracefully.
			if decoded, err := hex.DecodeString(string(resourceHash)); err == nil && len(decoded) == 32 {
				copy(ob.ResourceHash[:], decoded)
			}
		}

		if deliveredAt != nil {
			t := time.Unix(*deliveredAt, 0)
			ob.DeliveredAt = &t
		}

		if lockTx != nil {
			ob.LockTx = types.TxHash(*lockTx)
		}

		if settleTx != nil {
			ob.SettleTx = types.TxHash(*settleTx)
		}

		out = append(out, ob)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ledger: iterating rows: %w", err)
	}

	return out, nil
}
