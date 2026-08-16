# Leash: Architecture & Flow Diagrams

This document outlines the end-to-end architecture, request lifecycle, and component interactions of the **Leash L3 Autonomous Escrow & Recovery Protocol**.

---

## 1. High-Level System Architecture

```mermaid
flowchart TD
    subgraph Client ["Client Layer"]
        Agent["AI Agent / Caller"]
    end

    subgraph Sidecar ["Leash Sidecar Proxy"]
        Proxy["Proxy Handler (cmd/leash)"]
        Engine["Settlement Engine"]
        Breaker["Circuit Breaker"]
        Verifier["Deterministic Verifier"]
        Ledger["SQLite Ledger"]
        Sweeper["Autonomous Sweeper"]
        ChainSigner["Nonce Signer / Chain Client"]
    end

    subgraph Merchant ["Merchant Layer"]
        API["Mock Merchant API (cmd/mockmerchant)"]
    end

    subgraph Blockchain ["Monad Testnet"]
        Contract["PaymentEscrow Smart Contract"]
    end

    subgraph UI ["Observability"]
        Dashboard["Operator Dashboard (SSE /events)"]
    end

    Agent -->|"1. HTTP GET /resource"| Proxy
    Proxy --> Engine
    Engine --> Breaker
    Engine -->|"2. Initial Probe"| API
    API -->|"3. HTTP 402 + Challenge Terms"| Engine
    Engine -->|"4. Lock Funds"| ChainSigner
    ChainSigner -->|"Lock Tx"| Contract
    Engine -->|"Record Obligation"| Ledger
    Engine -->|"5. Paid Request + Escrow-ID"| API
    API -->|"6. Data Response"| Engine
    Engine -->|"7. Verify Body and Hash"| Verifier
    Engine -->|"8a. Release / 8b. Partial"| ChainSigner
    Sweeper -.->|"8c. Auto Refund on Absent"| ChainSigner
    Ledger -->|"Live Events"| Dashboard
```

---

## 2. End-to-End Request & Settlement Flow (Sequence Diagram)

```mermaid
sequenceDiagram
    autonumber
    actor Agent as AI Agent
    participant Leash as Leash Proxy / Engine
    participant Breaker as Circuit Breaker
    participant Ledger as SQLite Ledger
    participant Chain as PaymentEscrow Contract
    participant Merchant as Merchant API

    Agent->>Leash: GET /resource
    Leash->>Merchant: Forward GET /resource (Initial Probe)
    Merchant-->>Leash: HTTP 402 Payment Required (Price, Merchant, Hash)
    
    Leash->>Breaker: Check circuit health for Merchant
    alt Circuit OPEN (Too many failures)
        Breaker-->>Leash: Blocked
        Leash-->>Agent: HTTP 503 Service Unavailable (Funds Protected)
    else Circuit CLOSED (Healthy)
        Leash->>Chain: Lock Funds (Tx 1)
        Chain-->>Leash: EscrowID and TxHash
        Leash->>Ledger: Open Obligation (Status: LOCKED)
        
        Leash->>Merchant: Retry GET with Escrow ID and Lock Tx
        Merchant-->>Leash: Response Data (200 / 502 / Body)
        
        Leash->>Leash: Verify Response and Keccak256 Hash

        alt Case 1: Delivered (200 OK and Hash Match)
            Leash->>Chain: Release 100% to Merchant (Tx 2)
            Leash->>Ledger: Mark Settled (Status: RELEASED)
            Leash->>Breaker: Record Success
            Leash-->>Agent: 200 OK (Verified Data)
            
        else Case 2: Partial Delivery (Valid shape but hash mismatch)
            Leash->>Chain: Release Partial 50% split (Tx 2)
            Leash->>Ledger: Mark Settled (Status: PARTIAL)
            Leash->>Breaker: Record Failure
            Leash-->>Agent: 200 OK (Partial Data)
            
        else Case 3: Absent / Failed / Corrupted (5xx / Timeout / Empty)
            Leash->>Breaker: Record Failure
            Note over Leash,Chain: On-chain release is skipped. Funds stay locked.
            Leash-->>Agent: 502 Bad Gateway
            
            Note over Leash,Chain: Background Sweeper (after TTL expiry):
            Leash->>Chain: Refund 100% to Buyer (Tx 2)
            Leash->>Ledger: Mark Settled (Status: REFUNDED)
        end
    end
```

---

## 3. Core Component Breakdown

| Component | Path | Description |
| :--- | :--- | :--- |
| **Sidecar Server** | `cmd/leash/main.go` | HTTP reverse proxy, SSE streaming (`/events`), web dashboard routes, and management APIs. |
| **Settlement Engine** | `internal/engine/engine.go` | 402 challenge capture, escrow lifecycle execution, and automated sweeper loop. |
| **Deterministic Verifier** | `internal/engine/verify.go` | Verifies Keccak256 hash and response payload to produce a verdict (`Delivered`, `Partial`, `Absent`). |
| **Circuit Breaker** | `internal/engine/breaker.go` | Tracks merchant failure rates and trips (`503`) to protect agent funds from repeating failures. |
| **Chain Signer** | `internal/chain/escrow.go` | Manages nonces and executes smart contract transactions on Monad testnet. |
| **Smart Contract** | `contracts/src/PaymentEscrow.sol` | On-chain custody of escrow funds with programmatic release, partial release, and refund logic. |
| **SQLite Ledger** | `internal/ledger/ledger.go` | Persistent state storage for obligations (`LOCKED`, `RELEASED`, `REFUNDED`, `PARTIAL`). |
