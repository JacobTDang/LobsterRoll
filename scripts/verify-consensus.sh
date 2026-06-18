#!/usr/bin/env bash
# Headless proof that consensus fires end-to-end with NO real Telegram/RPC/keys:
# embedded NATS + mock Telegram + the real enrichment, consensus & notifier
# binaries. Injects 3 distinct-wallet trades on one token and asserts a
# "🔥 CONSENSUS" alert is delivered.
#
# Uses isolated ports (NATS 4223, mock 8098) so it can't collide with run-local
# or verify-alerts.
set -uo pipefail
cd "$(dirname "$0")/.."
mkdir -p .local
ALERTS=.local/consensus-alerts.log
: > "$ALERTS"
NURL="nats://localhost:4223"
# Fresh consensus DB each run: otherwise the prior run's in-window cohort + fired
# high-water mark correctly suppress a re-fire for the same wallets/token.
rm -f .local/c-consensus.db .local/c-consensus.db-wal .local/c-consensus.db-shm

pids=()
cleanup() { for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done; wait 2>/dev/null || true; }
trap cleanup EXIT

# Build real binaries (not `go run`, whose child orphans past the trap) and clear
# any stragglers from a prior run so ports are free.
echo ">> building"; make build >/dev/null
for t in natsd mocktelegram injecttrade; do go build -o "bin/$t" "./tools/$t"; done
pkill -f 'bin/natsd' 2>/dev/null || true; pkill -f 'bin/mocktelegram' 2>/dev/null || true; sleep 1

./bin/natsd -port 4223 >.local/c-natsd.log 2>&1 & pids+=($!)
./bin/mocktelegram -addr :8098 -out "$ALERTS" >.local/c-mocktg.log 2>&1 & pids+=($!)
sleep 2

ENRICHMENT_GRPC_ADDR=":50062" ENRICHMENT_DB_PATH=.local/c-enr.db ./bin/enrichment >.local/c-enr.log 2>&1 & pids+=($!)
CONSENSUS_DB_PATH=.local/c-consensus.db CONSENSUS_MIN_WALLETS=3 NATS_URL="$NURL" ./bin/consensus >.local/c-consensus.log 2>&1 & pids+=($!)
sleep 1
# LEADERBOARD_GRPC_ADDR points at localhost (fast connection-refused) so the
# best-effort whale-stats lookup fails instantly instead of a 10s DNS timeout.
TELEGRAM_BASE_URL="http://localhost:8098" TELEGRAM_BOT_TOKEN="test" TELEGRAM_CHAT_ID="1" \
  ENRICHMENT_GRPC_ADDR="localhost:50062" LEADERBOARD_GRPC_ADDR="localhost:50051" NATS_URL="$NURL" \
  ./bin/notifier >.local/c-notifier.log 2>&1 & pids+=($!)
sleep 2

echo ">> injecting 3 distinct-wallet trades on the same token+side"
./bin/injecttrade -nats "$NURL" -n 3 -side buy >/dev/null 2>&1

# Wait up to 15s for a CONSENSUS alert.
for _ in $(seq 1 30); do
  if grep -q "CONSENSUS" "$ALERTS" 2>/dev/null; then
    echo ">> PASS — consensus alert delivered:"
    echo "----------------------------------------"
    grep -A4 "CONSENSUS" "$ALERTS" | sed 's/^/   /'
    echo "----------------------------------------"
    exit 0
  fi
  sleep 0.5
done

echo ">> FAIL — no consensus alert within timeout"
echo "--- consensus log ---"; tail -5 .local/c-consensus.log
echo "--- notifier log ---"; tail -5 .local/c-notifier.log
exit 1
