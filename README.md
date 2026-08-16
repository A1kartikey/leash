# Leash: L3 Autonomous Escrow & Recovery Protocol for AI Agents

Leash is an autonomous L3 escrow sidecar proxy and smart contract protocol built for AI agent payments on **Monad Testnet**. It eliminates counterparty risk in agent-to-merchant API payments: when an agent pays a merchant for data/services via HTTP 402, funds are locked in an on-chain escrow contract. If the merchant fails to deliver or returns invalid/corrupted data, Leash's deterministic verifier detects the non-delivery and the automated sweeper refunds 100% of the funds back to the agent without human intervention.

---

## Architecture & How It Works

```
                        +----------------------------------------+
                        |           AI Agent (Client)            |
                        +----------------------------------------+
                                            |
                                 1. GET /resource
                                            v
+-----------------------------------------------------------------------------------+
|                            Leash Sidecar Proxy (:8080)                            |
|                                                                                   |
|  +---------------------+   2. Forward GET    +---------------------------------+  |
|  | Engine / Verifier   |-------------------->| Mock Merchant (:8081)           |  |
|  +---------------------+                     +---------------------------------+  |
|             |                                                |                    |
|       3. Lock Escrow                                   4. Return 402             |
|             |                                                |                    |
|             v                                                v                    |
|  +---------------------+   5. Retry + Escrow ID   +---------------------------------+  |
|  | SQLite Ledger       |------------------------->| Merchant Response               |  |
|  | (/tmp/leash.db)     |                          | (Honest 200 / Fail 502 / Partial|  |
|  +---------------------+                          +---------------------------------+  |
|             |                                                |                    |
|             |------------------ 6. Verify Body ------------------+                    |
|             |                                                                     |
|    +--------+--------------------------+-------------------------+                |
|    | (Delivered)                       | (Partial)               | (Absent)       |
|    v                                   v                         v                |
| 7. On-chain Release               7. On-chain Partial       7. Circuit Breaker +   |
|    (Merchant paid)                     (Split payout)            Sweeper Auto-Refund  |
+-----------------------------------------------------------------------------------+
       |                                   |                       |
       +-----------------------------------+-----------------------+
                                           |
                                           v
                        +----------------------------------------+
                        |  PaymentEscrow Contract (Monad Testnet)|
                        |  0x678d1Ef8835A51BCF215a9922c5775eF7D97C8A5 |
                        +----------------------------------------+
```

### Core Components & Verification Logic

1. **Deterministic Verification (`internal/engine/verify.go`)**:
   * **Delivered (`2xx` + matching Keccak256 hash)**: Triggers an on-chain `release(escrow_id)` paying 100% to merchant.
   * **Partial (`2xx` + hash mismatch but valid body shape)**: Triggers an on-chain `releasePartial(escrow_id, share)` splitting payment according to `PartialBps`.
   * **Absent (`5xx`, timeout, or empty body)**: Escrow remains locked on-chain. The merchant gets 0 MON.
2. **Autonomous Sweeper (`internal/engine/engine.go`)**:
   * Single ticker goroutine polling expired obligations (`deadline + SweepGrace`).
   * Executes an on-chain `refund(escrow_id)` back to the buyer wallet with zero manual input.
3. **Per-Tenant Circuit Breaker (`internal/engine/breaker.go`)**:
   * Tracks consecutive non-deliveries per `(tenant_id, merchant_address)`.
   * If threshold (default `3`) is reached, circuit opens (`503 Service Unavailable`) blocking new lock transactions to protect the agent's balance.
4. **Tenant-Scoped Ledger (`internal/ledger/sqlite.go`)**:
   * Idempotent SQLite store recording obligation state transitions (`locked`, `released`, `refunded`, `partial`).
   * Publishes real-time events to an internal event feed (`ledger.Feed`).
5. **Operator Dashboard & SSE API (`cmd/leash/main.go` & `web/index.html`)**:
   * Real-time web UI broadcasting balance changes, circuit state, and live transaction logs via Server-Sent Events (`/events`).

---

## Project Structure

```
.
├── cmd/
│   ├── leash/                 # Leash Sidecar Proxy & Operator Dashboard server
│   │   ├── main.go            # Proxy routing, engine setup, SSE streaming, API endpoints
│   │   └── demo.go            # In-memory mock chain for offline/demo mode (-mock)
│   ├── mockmerchant/          # Demo HTTP 402 Merchant server
│   │   └── main.go            # Serves /resource and runtime /mode switcher (honest|fail|partial)
│   └── agent/                 # Demo AI Agent client loop
│       └── main.go            # Autonomous buyer loop with stage control API (:8082)
├── internal/
│   ├── chain/                 # Ethereum client & contract binding wrapper
│   │   ├── config.go          # Config loader (reads .env and deployments.json)
│   │   ├── escrow.go          # Lock, Release, Refund, ReleasePartial, Claim transactions
│   │   ├── nonce.go           # Nonce-sequential signer loop
│   │   ├── subscribe.go       # Event subscription and block poller
│   │   └── bindings/          # Generated abigen Go bindings for PaymentEscrow
│   ├── engine/                # Core settlement & verification engine
│   │   ├── engine.go          # Fetch lifecycle pipeline & RunSweeper loop
│   │   ├── verify.go          # Pure Judge() verification function & HTTP verifier
│   │   ├── breaker.go         # Per-tenant per-merchant Circuit Breaker
│   │   └── challenge.go       # HTTP 402 header parser & builder
│   ├── ledger/                # Persistent obligation store
│   │   ├── sqlite.go          # SQLite database schema and queries
│   │   └── feed.go            # Go channel pub/sub feed for UI events
│   └── types/                 # Domain types, interfaces, and mock implementations
├── contracts/
│   ├── src/
│   │   └── PaymentEscrow.sol  # Solidity smart contract custodying MON escrow funds
│   ├── test/                  # Foundry unit and fuzz tests (57 tests passing)
│   └── foundry.toml           # Foundry project configuration
├── web/
│   └── index.html             # Glassmorphism real-time operator dashboard UI
├── deployments.json           # Single source of truth for contract addresses
├── .env                       # Local environment configuration (RPC, keys)
├── .env.example               # Configuration template
└── README.md                  # System architecture & execution manual
```

---

## Setup & Prerequisites

### Requirements
* **Go**: `v1.22+` (or latest standard Go toolchain)
* **Foundry (`cast` & `forge`)**: Installed at `~/.foundry/bin/cast` (optional, for contract interaction)

### Standard Setup Commands

```bash
# 1. Clone repository and navigate to root
cd /home/kartikey/Leash

# 2. Download Go dependencies
go mod download

# 3. Verify smart contract tests (optional)
cd contracts && forge test && cd ..
```

---

## Configuration (`.env` & `deployments.json`)

All runtime settings are loaded via `.env` and `deployments.json`.

### `.env` File Structure
```env
# Monad Testnet RPC URL
MONAD_RPC=https://testnet-rpc.monad.xyz

# Buyer Agent ECDSA Private Key (Hex)
BUYER_KEY=04d36d615cadcd185b5ca80b28cd15f000979345d2431af77f4326c7f48bf87b

# Merchant / Seller ECDSA Private Key (Hex)
MERCHANT_KEY=7c59a7e5a2556e775a74941f0950ff3a4771d89526c59b6f7eb36ebb66bff57a

# Default Escrow Release TTL in seconds
LEASH_DEFAULT_TTL=60
```

### Explanation of Config Keys

| Variable | Description | Source |
| :--- | :--- | :--- |
| `MONAD_RPC` | RPC endpoint for Monad Testnet (Chain ID `10143`). | `.env` |
| `BUYER_KEY` | Hex private key powering the agent buyer sidecar wallet. | `.env` |
| `MERCHANT_KEY` | Hex private key powering the merchant wallet. | `.env` |
| `LEASH_DEFAULT_TTL` | Escrow lock duration in seconds before sweeper refund is eligible. | `.env` |
| `escrow_address` | Contract address deployed on Monad testnet (`0x678d1Ef8835A51BCF215a9922c5775eF7D97C8A5`). | `deployments.json` |

---

## Key Network Endpoints & Ports

| Component | Host / Port | Route / Resource | Description |
| :--- | :--- | :--- | :--- |
| **Leash Sidecar Proxy** | `http://localhost:8080` | `/` | Web Operator Dashboard |
| | `http://localhost:8080` | `/events` | Server-Sent Events (SSE) real-time feed |
| | `http://localhost:8080` | `/api/state` | JSON snapshot of balances & obligations |
| | `http://localhost:8080` | `/resource` | Forwarded upstream through settlement path |
| **Mock Merchant** | `http://localhost:8081` | `/resource` | Serves HTTP 402 challenge & paid payload |
| | `http://localhost:8081` | `/mode` | GET/POST runtime mode switcher (`honest`, `fail`, `partial`) |
| **Agent Controller** | `http://localhost:8082` | `/pause`, `/resume`, `/toggle` | Pause / resume buying loop |
| | `http://localhost:8082` | `/status` | Query current buyer loop state |
| **PaymentEscrow Contract** | Monad Testnet | `0x678d1Ef8835A51BCF215a9922c5775eF7D97C8A5` | On-chain MON custody & settlement contract |

---

## Complete Demo Execution Guide

Follow this step-by-step procedure to run the full live demo on **Monad Testnet**.

### Step 1: Start the Mock Merchant Server

* **Directory**: `/home/kartikey/Leash`
* **Command**:
  ```bash
  go run ./cmd/mockmerchant -addr :8081 -merchant 0x766781E665a18723146FF6AFCE30cFD3a5F2b55c -price 1000000000000000 -mode honest
  ```
* **What it starts**: Runs the HTTP 402 merchant server on port `:8081` configured to charge `0.001 MON` per resource.
* **Expected Output**:
  ```text
  mockmerchant on :8081: mode=honest price=1000000000000000 wei merchant=0x766781E665a18723146FF6AFCE30cFD3a5F2b55c
  ```
* **Next Step**: Open a new terminal tab/window.

---

### Step 2: Start the Leash Sidecar Proxy & Dashboard

* **Directory**: `/home/kartikey/Leash`
* **Command**:
  ```bash
  set -a && source .env && set +a
  go run ./cmd/leash -addr :8080 -upstream http://localhost:8081 -ttl 60s -sweep-interval 5s -breaker-threshold 3
  ```
* **What it starts**: Starts the Leash proxy on `:8080`, initializes the Monad testnet chain connection, connects SQLite ledger at `/tmp/leash.db`, and launches the background sweeper loop.
* **Expected Output**:
  ```text
  leash on :8080: tenant=agent-1 buyer=0xAa656533b9B29f2d2ACb7444c75859355BE514fe upstream=http://localhost:8081 contract=0x678d1Ef8835A51BCF215a9922c5775eF7D97C8A5
  settlement: ttl=1m0s sweep=5s+5s breaker=3/30s
  ```
* **Next Step**: Open browser to `http://localhost:8080` to view the live dashboard UI, then open a third terminal tab.

---

### Step 3: Start the Autonomous AI Agent Buyer

* **Directory**: `/home/kartikey/Leash`
* **Command**:
  ```bash
  go run ./cmd/agent -proxy http://localhost:8080 -url http://localhost:8080/resource -n 0 -pace 3s -control :8082
  ```
* **What it starts**: Launches the agent buying loop sending requests through the proxy every 3 seconds with control endpoint on `:8082`.
* **Expected Output**:
  ```text
  control on :8082: POST /pause /resume /toggle, GET /status
  buying through the Leash proxy: http://localhost:8080/resource
  resource 0 try 0: delivered delivered        200, 88 bytes
  ```
* **Next Step**: Observe `Delivered` transactions on the UI dashboard. The merchant balance increases by `0.001 MON` per request.

---

### Step 4: Demonstrate Merchant Failure & Autonomous Refund

* **Directory**: `/home/kartikey/Leash` (Terminal 4)
* **Command**:
  ```bash
  curl -X POST http://localhost:8081/mode -d fail
  ```
* **What it changes**: Switches the merchant into `fail` mode. The merchant takes the on-chain lock transaction but returns HTTP 502 with 0 bytes (`Absent`).
* **Expected Behavior**:
  1. Agent log shows: `resource N try 0: absent bad_status 502, 0 bytes`.
  2. Funds remain locked in escrow contract (`Status: Locked`).
  3. After 60 seconds (TTL), the Leash sweeper automatically fires an on-chain `refund()` transaction.
  4. The dashboard updates: `Recovered MON` increases by `0.001 MON`, buyer spendable balance is restored, and merchant receives `0 MON`.
  5. After 3 consecutive failures, the Circuit Breaker trips to `OPEN`, blocking further lock attempts with `503 Service Unavailable`.

---

### Step 5: Demonstrate Partial Payout

* **Directory**: `/home/kartikey/Leash` (Terminal 4)
* **Command**:
  ```bash
  curl -X POST http://localhost:8081/mode -d partial
  ```
* **What it changes**: Switches merchant to `partial` mode (returns truncated response).
* **Expected Behavior**: Leash verifier marks response `Partial`, issues an on-chain `releasePartial()`, splitting 50% to merchant and returning 50% to buyer.

---

### Step 6: Reset / Restore Merchant to Honest Mode

* **Directory**: `/home/kartikey/Leash` (Terminal 4)
* **Command**:
  ```bash
  curl -X POST http://localhost:8081/mode -d honest
  ```
* **What it changes**: Resets merchant back to `honest` mode. Once breaker cooldown expires (30s), normal operations resume.

---

## How to Reset / Restart Demo

To perform a clean reset of all local states:

```bash
# 1. Stop all running processes (Ctrl+C in terminals 1, 2, 3)

# 2. Remove local SQLite database file
rm -f /tmp/leash.db /tmp/leash-agent.db

# 3. Restart processes from Step 1 through Step 3
```

---

## 5-Minute Hackathon Demo Procedure

For a live presentation or video walk-through, follow this concise script:

1. **Show Setup**: Open `http://localhost:8080` in browser. Point out the buyer's spendable MON balance and contract address `0x678d1Ef8835A5...`.
2. **Happy Path (Honest Delivery)**: Start agent buying loop (`go run ./cmd/agent ...`). Watch 2 successful purchases complete. Show on-chain `Release` events in feed and merchant balance increasing.
3. **Simulate Adversarial Merchant**: Run `curl -X POST localhost:8081/mode -d fail`. Watch the merchant accept payment lock but deliver nothing (`502`). Point out that merchant received `0 MON`.
4. **Autonomous Sweeper Recovery**: Wait ~60s for TTL. Show the live feed event `refund` firing automatically on Monad testnet, restoring `Recovered MON` to buyer wallet without touching a key.
5. **Circuit Breaker Defense**: Show after 3 failures the circuit state switches to `OPEN` (red badge in UI), returning `503 Service Unavailable` and preventing the agent from burning MON on a non-responsive merchant.

---

## Quick Start

Copy-paste sequence to launch the full stack in 3 terminal windows:

### Terminal 1: Merchant
```bash
cd /home/kartikey/Leash
go run ./cmd/mockmerchant -addr :8081 -merchant 0x766781E665a18723146FF6AFCE30cFD3a5F2b55c -price 1000000000000000 -mode honest
```

### Terminal 2: Leash Proxy & Dashboard
```bash
cd /home/kartikey/Leash
set -a && source .env && set +a
go run ./cmd/leash -addr :8080 -upstream http://localhost:8081 -ttl 60s -sweep-interval 5s
```

### Terminal 3: AI Agent Buyer
```bash
cd /home/kartikey/Leash
go run ./cmd/agent -proxy http://localhost:8080 -url http://localhost:8080/resource -n 0 -pace 3s -control :8082
```

---

## Full Execution Flow

```text
START
  │
  ├── [1] Start Mock Merchant (:8081)
  │
  ├── [2] Source .env & Start Leash Proxy (:8080)
  │
  ├── [3] Open Dashboard UI (http://localhost:8080)
  │
  ├── [4] Start AI Agent Loop (:8082)
  │       ├── Requests /resource via Proxy
  │       ├── Receives HTTP 402 Payment Challenge
  │       ├── Escrow Locked on Monad Testnet Contract (0x678d1Ef...)
  │       └── Verifier approves -> On-chain Release (Honest Payout)
  │
  ├── [5] Trigger Failure Mode (`curl -X POST localhost:8081/mode -d fail`)
  │       ├── Merchant returns 502 / Empty Body
  │       ├── Verifier flags Absent -> Lock untouched
  │       └── Sweeper polls TTL -> Auto-Refund Executed on-chain
  │
  └── [6] Circuit Breaker Trips (OPEN -> 503 Service Unavailable)
          └── Protects Agent from compounding financial loss
  │
FINAL DEMO COMPLETE
```

---

## Troubleshooting

### 1. `MONAD_RPC and BUYER_KEY must be set` or `LoadConfig error`
* **Cause**: `.env` file not sourced or missing in terminal session.
* **Fix**: Run `set -a && source .env && set +a` before running `go run ./cmd/leash` or `go test`.

### 2. `lock tx reverted` or `insufficient funds`
* **Cause**: Buyer wallet balance is too low or nonce sequence desynchronized.
* **Fix**: Verify wallet balance with `cast balance 0xAa656533b9B29f2d2ACb7444c75859355BE514fe --rpc-url https://testnet-rpc.monad.xyz`. Ensure price per request is small (`0.001 MON` = `1000000000000000` wei).

### 3. `address already in use :8080 / :8081 / :8082`
* **Cause**: A previous instance of `leash`, `mockmerchant`, or `agent` is still running in background.
* **Fix**: Kill existing Go processes:
  ```bash
  pkill -f "go run ./cmd"
  ```

### 4. Circuit Breaker stuck in `OPEN`
* **Cause**: Circuit breaker opened after 3 consecutive failures and cooldown is active.
* **Fix**: Switch merchant back to honest mode (`curl -X POST localhost:8081/mode -d honest`) and wait 30 seconds for breaker cooldown to reset to `HALF-OPEN` / `CLOSED`.

### 5. Dashboard UI not updating / SSE disconnected
* **Cause**: Browser lost SSE connection to `http://localhost:8080/events`.
* **Fix**: Refresh browser tab at `http://localhost:8080`.
