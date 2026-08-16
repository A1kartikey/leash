#!/usr/bin/env bash
# Production Render startup script for Leash on real Monad Testnet.
# Runs Merchant (:8081) and AI Agent (pace: 3s) in background,
# and Leash Proxy & Dashboard on $PORT in foreground.
set -euo pipefail

PORT="${PORT:-8085}"
MERCHANT_PORT="${MERCHANT_PORT:-8081}"

echo "================================================="
echo "  🚀 Starting Leash Rig on Monad Testnet"
echo "================================================="
echo "Proxy / Dashboard Port : ${PORT}"
echo "Merchant Port          : ${MERCHANT_PORT}"
echo "Monad RPC              : ${MONAD_RPC:-https://testnet-rpc.monad.xyz}"
echo "================================================="

# Build binaries if not already built
mkdir -p bin
[ -f bin/mockmerchant ] || go build -o bin/mockmerchant ./cmd/mockmerchant
[ -f bin/leash ] || go build -o bin/leash ./cmd/leash
[ -f bin/agent ] || go build -o bin/agent ./cmd/agent

ESCROW="${LEASH_ESCROW:-0x678d1Ef8835A51BCF215a9922c5775eF7D97C8A5}"
RPC="${MONAD_RPC:-https://testnet-rpc.monad.xyz}"

# 1. Start Merchant in background
echo "[1/3] Starting Mock Merchant on :${MERCHANT_PORT}..."
./bin/mockmerchant \
  -addr ":${MERCHANT_PORT}" \
  -mode honest \
  -rpc "${RPC}" \
  -escrow "${ESCROW}" &
MERCHANT_PID=$!

sleep 1

# 2. Start Agent buyer loop in background (requests data every 3s via proxy)
echo "[2/3] Starting AI Agent Buyer Loop (Pace: 3s)..."
./bin/agent \
  -proxy "http://localhost:${PORT}" \
  -url /resource \
  -n 0 \
  -pace 3s &
AGENT_PID=$!

# 3. Start Leash in foreground (Real Monad Testnet mode, no -mock flag)
echo "[3/3] Starting Leash Proxy & Dashboard on :${PORT}..."
exec ./bin/leash \
  -addr ":${PORT}" \
  -upstream "http://localhost:${MERCHANT_PORT}"
