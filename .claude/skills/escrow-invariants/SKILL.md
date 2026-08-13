---
name: escrow-invariants
description: Test discipline for PaymentEscrow.sol. Use whenever editing, reviewing, or adding functions to the escrow contract, writing Foundry tests for it, or when the user mentions lock, release, refund, claim, releasePartial, reentrancy, or the escrow contract.
---

# PaymentEscrow test discipline

This contract is a public singleton holding every user's locked funds at one address.
A defect drains everyone, not one customer. There is no owner, no pause, and no
upgrade path — a bug means a v2 at a new address and a migration.

**No contract change is complete until every invariant below has a passing test.**

## Invariants — assert all of these after any state-changing function

1. `address(this).balance == Σ amount of all escrows in status Locked`
   Assert before and after every operation. This is the master invariant; if it
   holds, funds cannot be lost or double-spent.
2. No transition out of `Released` or `Refunded`. Both are terminal. Test that
   every entrypoint reverts when called on a terminal escrow.
3. `releasePartial(id, amount)` requires `amount <= e.amount`, and the remainder
   always returns to the buyer in the same transaction. Sum of (merchant payout +
   buyer remainder) must equal the original escrow amount exactly.
4. `lock()` enforces `releaseDeadline < claimDeadline`. Test the boundary.
5. `refund()` reverts before `releaseDeadline`. `claim()` reverts before
   `claimDeadline`. Test one second either side of both.
6. Only `e.buyer` may call `release`/`refund`/`releasePartial`. Only `e.merchant`
   may call `claim`. Test with a third address for each.

## Reentrancy — mandatory, not optional

Every payout path (`release`, `refund`, `releasePartial`, `claim`, and every
batch variant) must be tested against a malicious receiver contract whose
`receive()` re-enters the same function and the sibling functions.

Required pattern in the contract:

- Set `status` to its terminal value **before** any value transfer
- Checks → effects → interactions, no exceptions
- `ReentrancyGuard` on every payout entrypoint
- Payout via `.call{value:}` with the return value checked

## Batch functions

`lockBatch`, `releaseMany`, `refundMany` must each be tested for:

- Array length mismatch reverts
- Partial failure behaviour is explicit and documented (all-or-nothing preferred)
- The master balance invariant holds across the whole batch, not just per element
- Gas does not grow unbounded — cap array length in the contract

## Events

`EscrowLocked` must index `buyer` and `merchant`. The off-chain sidecar discovers
its escrows by filtering on the buyer address; without both indexed, discovery
breaks. Every state transition emits exactly one event.

## Before saying the contract is done

```
forge test -vvv          # all green, including reentrancy and boundary cases
forge coverage           # every branch of every entrypoint hit
forge test --gas-report  # no unbounded loops
```

Then confirm by reading the source, not by assumption: no `owner`, no `onlyOwner`,
no `selfdestruct`, no `delegatecall`, no proxy pattern, no fee variable, no pause.