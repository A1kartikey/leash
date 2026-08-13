---
name: monad-deploy
description: Deploy and verify PaymentEscrow on Monad testnet and propagate the new address. Use when deploying a contract, redeploying after a contract change, verifying source on the explorer, regenerating Go bindings, or when the user mentions deploy, forge script, abigen, or the escrow address.
---

# Deploy PaymentEscrow to Monad

Three people share one deployed address. A redeploy that isn't propagated breaks
everyone silently — the sidecar talks to a stale contract and every lock lands
somewhere nobody is watching. **Deploy and propagation are one procedure, not two.**

## Preconditions

- `forge test` fully green, including the reentrancy suite (see escrow-invariants)
- Deployer wallet funded from the Monad testnet faucet
- `.env` present and not committed: `MONAD_RPC`, `DEPLOYER_KEY`

## Procedure

**1. Deploy**

```
forge script script/Deploy.s.sol \
  --rpc-url $MONAD_RPC \
  --private-key $DEPLOYER_KEY \
  --broadcast
```

Record the deployed address and the deployment transaction hash.

**2. Verify source on the explorer.** Unverified source means merchants cannot
audit the contract, and merchant auditability is the entire adoption argument.
This step is not optional and is not deferred.

**3. Regenerate Go bindings** — the ABI has changed if the contract has:

```
forge inspect PaymentEscrow abi > internal/chain/abi/PaymentEscrow.json
abigen --abi internal/chain/abi/PaymentEscrow.json \
       --pkg bindings --type PaymentEscrow \
       --out internal/chain/bindings/payment_escrow.go
```

**4. Propagate the address to every place that holds it:**

- `deployments.json` at repo root — the single source of truth, committed
- `.env.example` — updated, no real keys
- The UI's explorer link
- The merchant SDK's default config
- Any test fixture that hardcodes it

**5. Smoke test against the live deployment** before telling anyone it's ready:
lock → release on one escrow, lock → wait → refund on another. Both transactions
confirmed on the explorer.

**6. Announce** the new address, the tx hash, and what changed. If teammates have
pending local state pointing at the old contract, they need to know.

## Redeploy discipline

The contract is immutable. A redeploy is a new address and a migration, never an
upgrade. Any escrow still `Locked` on the old contract must be refunded or claimed
there — the new contract knows nothing about it. Check for outstanding locked
escrows on the old address before abandoning it.

## Never

- Deploy from a key that also holds demo funds
- Deploy without a green test suite
- Skip explorer verification "for now"
- Hardcode the address anywhere except `deployments.json`