---
name: leash-engine
description: Correctness rules for the Leash Go sidecar — nonce handling, escrow lifecycle, ledger durability, event discovery, tenancy scoping. Use when writing or reviewing code in internal/chain, internal/ledger, internal/engine, or internal/tenant, or when the user mentions the sweeper, circuit breaker, obligation ledger, restart recovery, or multi-tenancy.
---

# Leash engine correctness rules

Four mistakes in this codebase are cheap to prevent and expensive to fix once the
engine is stateful. Check every change against all four.

## 1. Serialise nonces per signer

Concurrent `lock()` calls from one signer collide and one silently fails. Do not
rely on the node's pending nonce under concurrency.

Each tenant's signer is owned by exactly one goroutine that holds its nonce and
submits transactions sequentially from a channel. Callers send a request and wait
on a reply channel. No other code path may sign for that tenant.

## 2. Sweeper, never timer-per-obligation

A `time.AfterFunc` per escrow leaks goroutines under load and dies with the
process — and dying with the process defeats the entire durability story.

One sweeper goroutine polls `ix_pending` on an interval, finds obligations past
`release_deadline` still in `LOCKED`, and refunds them. Restart recovery then
requires no special code: the sweeper simply finds them on the next tick.

## 3. Discover escrows by event filter, never by ID range

Escrow IDs come from a counter shared with every other user of the public
singleton. Between two of your locks, strangers will have taken IDs.

Subscribe to `EscrowLocked` filtered on the buyer address. Never assume IDs are
contiguous, never iterate a range, never infer "my next ID." This bug is invisible
in single-user testing and breaks the moment a second user exists.

## 4. Scope everything by tenant

Every ledger query, every circuit-breaker counter, every balance snapshot takes
`tenant_id`. A function that touches state without a tenant parameter is a bug.
Keys never cross tenants; a tenant's signer is reachable only through its own
tenant record.

Retrofitting this is a rewrite. Write it in from the first line.

## Ledger durability

`escrow_id` is the primary key — that unique constraint *is* the deduplication
guarantee, replacing the in-memory cache whose failure is the vulnerability class
this product exists to address. `Open()` must be idempotent: a duplicate insert is
a no-op, not an error.

Wei is `TEXT` in SQLite and `*big.Int` in Go. Never `REAL`, never `INTEGER`,
never `float64`.

**Test it by killing the process mid-flight.** Start N locks, kill, restart, and
assert every obligation still settles correctly with its original deadline. If that
test doesn't exist, durability is a claim rather than a property.

## Verifier stays pure

```
2xx  AND  len(body) > 0  AND  (keccak256(body) == resourceHash
                               OR satisfies declared content contract)
```

No I/O, no model call, no heuristic, no scoring. Returns
`Verdict{Delivered|Partial|Absent}` plus a reason code. It must be testable as a
pure function with table-driven tests. Any pressure to make verification "smarter"
belongs outside the settlement path.

## Circuit breaker

Counter keyed on `(tenant_id, merchant)`. Consecutive non-deliveries only —
a success resets it. At threshold the circuit opens and calls fail fast with 503
**before any funds are locked**. Half-open probe after cooldown. Breaker state is
per-tenant and never shared.