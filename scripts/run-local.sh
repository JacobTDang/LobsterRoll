#!/usr/bin/env bash
# Run the read + alert + approve pipeline locally (NO execution / no trader).
# Safe from the US: everything here is public reads + your own Telegram bot.
#
#   bash scripts/run-local.sh
#
# Reads .env if present. Required for the full alert path:
#   TELEGRAM_BOT_TOKEN, TELEGRAM_CHAT_ID   (notifier -> your phone)
#   RPC_WSS_URL                            (watcher -> real whale trades)
# Without RPC_WSS_URL the watcher is skipped; use `make inject-trade` to test alerts.
set -euo pipefail
cd "$(dirname "$0")/.."

if [ -f .env ]; then set -a; . ./.env; set +a; fi
mkdir -p .local
NATS_URL="${NATS_URL:-nats://localhost:4222}"

pids=()
cleanup() { echo; echo ">> stopping"; for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done; wait 2>/dev/null || true; }
trap cleanup EXIT INT TERM

echo ">> building services"; make build >/dev/null

echo ">> natsd (embedded NATS, no docker)"; go run ./tools/natsd & pids+=($!)
sleep 1

echo ">> leaderboard (gRPC :50051)"
LEADERBOARD_GRPC_ADDR=":50051" LEADERBOARD_DB_PATH=.local/leaderboard.db NATS_URL="$NATS_URL" ./bin/leaderboard & pids+=($!)

echo ">> enrichment (gRPC :50052)"
ENRICHMENT_GRPC_ADDR=":50052" ENRICHMENT_DB_PATH=.local/enrichment.db ./bin/enrichment & pids+=($!)
sleep 2

echo ">> strategy"
NATS_URL="$NATS_URL" ./bin/strategy & pids+=($!)

echo ">> consensus (multiple tracked wallets converging on one bet)"
CONSENSUS_DB_PATH=.local/consensus.db NATS_URL="$NATS_URL" ./bin/consensus & pids+=($!)

if [ -n "${TELEGRAM_BOT_TOKEN:-}" ] && [ -n "${TELEGRAM_CHAT_ID:-}" ]; then
  echo ">> notifier (Telegram alerts + approval + consensus)"
  ENRICHMENT_GRPC_ADDR="localhost:50052" LEADERBOARD_GRPC_ADDR="localhost:50051" NATS_URL="$NATS_URL" ./bin/notifier & pids+=($!)
else
  echo "!! notifier SKIPPED — set TELEGRAM_BOT_TOKEN + TELEGRAM_CHAT_ID in .env for phone alerts"
fi

if [ -n "${RPC_WSS_URL:-}" ]; then
  echo ">> watcher (live OrderFilled via WSS)"
  RPC_WSS_URL="$RPC_WSS_URL" LEADERBOARD_GRPC_ADDR="localhost:50051" NATS_URL="$NATS_URL" WATCHER_DB_PATH=.local/watcher.db ./bin/watcher & pids+=($!)
else
  echo "!! watcher SKIPPED — set RPC_WSS_URL for real whale trades; use 'make inject-trade' to test the alert path"
fi

echo ">> trader: NOT started (execution disabled)"
echo ">> running — Ctrl-C to stop. (logs are JSON, interleaved)"
wait
