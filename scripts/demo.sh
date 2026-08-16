#!/usr/bin/env bash
# One command to bring the whole rig up in the right order:
#
#   mock merchant -> leash proxy (dashboard + SSE) -> buyer agent
#
# The agent buys *through* the proxy, so the ledger the dashboard reads is the
# one the settlement path writes. Ctrl-C stops all three.
#
#   scripts/demo.sh            # in-memory chain, no funds needed
#   DEMO_MOCK=0 scripts/demo.sh   # same rig against Monad testnet (.env keys)
set -euo pipefail

cd "$(dirname "$0")/.."
[ -f demo.env ] || { echo "demo.env not found"; exit 1; }
set -a; . ./demo.env; [ -f .env ] && . ./.env; set +a

mkdir -p .demo
rm -f "${DEMO_DB}" "${DEMO_DB}"-wal "${DEMO_DB}"-shm

pids=()
cleanup() {
  trap - EXIT INT TERM
  echo
  echo "--- stopping demo rig ---"
  for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done
  wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

# prefix tags every line so one terminal reads as three. Output goes through a
# process substitution rather than a pipeline, so $! is the binary's own pid —
# kill a pipeline's pid and the process inside it survives to hold the port
# hostage on the next rehearsal.
run() {
  local tag=$1; shift
  "$@" > >(sed -u "s/^/[$tag] /") 2>&1 &
  pids+=($!)
}

# A port already in use means a stale rig from the last rehearsal — and a demo
# that silently talks to a stranger's process is worse than one that refuses to
# start. Check before anything is launched.
free_port() {
  local port=${1#:}
  if (exec 3<>"/dev/tcp/127.0.0.1/${port}") 2>/dev/null; then
    exec 3<&- 3>&-
    echo "port ${port} is already in use — stop the old rig first (pkill -f .demo/)"
    exit 1
  fi
}
for p in "${DEMO_MERCHANT_ADDR}" "${DEMO_LEASH_ADDR}" "${DEMO_CONTROL_ADDR}"; do free_port "$p"; done

# wait_port blocks until something is listening, so start order is real
# ordering and not a sleep-and-hope.
wait_port() {
  local url=$1 name=$2
  for _ in $(seq 1 60); do
    if curl -fsS -o /dev/null "$url" 2>/dev/null; then return 0; fi
    sleep 0.25
  done
  echo "$name never came up at $url"; exit 1
}

mock_flag=""
merchant_chain=()
if [ "${DEMO_MOCK}" = "1" ]; then
  mock_flag="-mock"
else
  # Real chain: point the merchant's SDK at the deployed escrow, so the demo
  # gates delivery on a confirmed lock rather than on trust.
  escrow="${LEASH_ESCROW:-$(sed -n 's/.*"escrow_address": *"\([^"]*\)".*/\1/p' deployments.json)}"
  merchant_chain=(-rpc "${MONAD_RPC}" -escrow "${escrow}")
fi

echo "--- building ---"
go build -o .demo/mockmerchant ./cmd/mockmerchant
go build -o .demo/leash ./cmd/leash
go build -o .demo/agent ./cmd/agent

echo "--- 1/3 mock merchant on ${DEMO_MERCHANT_ADDR} ---"
run merchant .demo/mockmerchant \
  -addr "${DEMO_MERCHANT_ADDR}" \
  -mode "${DEMO_START_MODE}" \
  -price "${DEMO_PRICE_WEI}" \
  "${merchant_chain[@]}"
wait_port "http://localhost${DEMO_MERCHANT_ADDR}/mode" "mock merchant"

echo "--- 2/3 leash proxy + dashboard on ${DEMO_LEASH_ADDR} ---"
run leash .demo/leash $mock_flag \
  -addr "${DEMO_LEASH_ADDR}" \
  -upstream "http://localhost${DEMO_MERCHANT_ADDR}" \
  -tenant "${DEMO_TENANT}" \
  -db "${DEMO_DB}" \
  -ttl "${DEMO_TTL}" \
  -sweep-interval "${DEMO_SWEEP_INTERVAL}" \
  -sweep-grace "${DEMO_SWEEP_GRACE}" \
  -breaker-threshold "${DEMO_BREAKER_THRESHOLD}" \
  -breaker-cooldown "${DEMO_BREAKER_COOLDOWN}"
wait_port "http://localhost${DEMO_LEASH_ADDR}/api/state" "leash"

echo "--- 3/3 buyer agent, pace ${DEMO_PACE} ---"
run agent .demo/agent \
  -proxy "http://localhost${DEMO_LEASH_ADDR}" \
  -url /resource \
  -n "${DEMO_N}" \
  -retries "${DEMO_RETRIES}" \
  -pace "${DEMO_PACE}" \
  -control "${DEMO_CONTROL_ADDR}"

cat <<EOF

--- rig up ---------------------------------------------------------------
  dashboard   http://localhost${DEMO_LEASH_ADDR}

  merchant    curl -X POST localhost${DEMO_MERCHANT_ADDR}/mode -d honest
              curl -X POST localhost${DEMO_MERCHANT_ADDR}/mode -d fail
              curl -X POST localhost${DEMO_MERCHANT_ADDR}/mode -d partial

  agent       curl -X POST localhost${DEMO_CONTROL_ADDR}/toggle    # pause/resume
              curl localhost${DEMO_CONTROL_ADDR}/status

  ttl ${DEMO_TTL} · breaker ${DEMO_BREAKER_THRESHOLD} · pace ${DEMO_PACE}      Ctrl-C stops everything
--------------------------------------------------------------------------

EOF

wait
