#!/usr/bin/env bash
# Headless end-to-end check of the alert path with NO real Telegram/RPC/keys:
# embedded NATS + a mock Telegram + the real enrichment & notifier binaries.
# Injects a synthetic trade and asserts a formatted alert was "sent".
set -uo pipefail
cd "$(dirname "$0")/.."
mkdir -p .local
ALERTS=.local/alerts.log
: > "$ALERTS"

pids=()
cleanup() { for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done; wait 2>/dev/null || true; }
trap cleanup EXIT

echo ">> building"; make build >/dev/null && go build ./tools/...

go run ./tools/natsd -port 4222 >.local/natsd.log 2>&1 & pids+=($!)
go run ./tools/mocktelegram -addr :8099 -out "$ALERTS" >.local/mocktg.log 2>&1 & pids+=($!)
sleep 2

ENRICHMENT_GRPC_ADDR=":50052" ENRICHMENT_DB_PATH=.local/verify-enr.db ./bin/enrichment >.local/enr.log 2>&1 & pids+=($!)
sleep 1
TELEGRAM_BASE_URL="http://localhost:8099" TELEGRAM_BOT_TOKEN="test" TELEGRAM_CHAT_ID="1" \
  ENRICHMENT_GRPC_ADDR="localhost:50052" NATS_URL="nats://localhost:4222" \
  ./bin/notifier >.local/notifier.log 2>&1 & pids+=($!)
sleep 2

echo ">> injecting synthetic trade"
go run ./tools/injecttrade -nats nats://localhost:4222 >/dev/null 2>&1

# Wait up to 15s for the mock Telegram to record an alert.
for _ in $(seq 1 30); do
  if [ -s "$ALERTS" ]; then
    echo ">> PASS — alert delivered to (mock) Telegram:"
    echo "----------------------------------------"
    sed 's/^/   /' "$ALERTS"
    echo "----------------------------------------"
    exit 0
  fi
  sleep 0.5
done

echo ">> FAIL — no alert recorded within timeout"
echo "--- notifier log ---"; tail -5 .local/notifier.log
exit 1
