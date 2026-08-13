Leash

Escrow-backed delivery verification for autonomous agent payments. Monad testnet.

The invariant the whole product exists to enforce: funds move to a merchant only after this system has independently verified that a valid response was delivered.

Layout
contracts/      PaymentEscrow.sol + Foundry tests
internal/chain/ Go binding wrapper (abigen output + wrapper)
internal/ledger/ SQLite obligation store
internal/engine/ 402 handling, lifecycle, verifier, circuit breaker
internal/tenant/ per-agent key + policy isolation
sdk/            merchant-side middleware
cmd/leash/      proxy entrypoint
cmd/mockmerchant/, cmd/agent/   demo rig (throwaway)
web/            single-page UI, SSE
Non-negotiables

Contract is a public singleton. No owner, no admin function, no protocol fee, no upgrade path, no pause. Credible neutrality is the adoption argument — never add an owner "just for now."

Access control is msg.sender == e.buyer. No delegate concept, no registry. Leash runs with the buyer's own key.

Verification is deterministic only. 2xx AND non-empty body AND hash match (or declared content contract). No LLM, no semantic scoring, no probabilistic judgement anywhere in the settlement path.

Native MON. No ERC-20 is written or deployed. lock() is payable.

Conventions
Go 1.22+, standard library HTTP. go-ethereum for chain access.
Wei is *big.Int in Go, TEXT in SQLite. Never float, never int64.
Every ledger row, breaker counter, and balance query is scoped by tenant_id.
Errors wrap with %w. No panics outside main.
Keys come from env / tenant config only. Never a literal, never committed.
Out of scope — do not build

Custom ERC-20 · facilitator integration · real x402 SDK · dispute resolution · arbiters · LLM verification · Postgres · Docker · auth beyond tenant API keys.