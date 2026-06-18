#!/usr/bin/env bash
# Start the read -> alert pipeline as an all-day DRY RUN (no trader, nothing
# executes). Services are detached (nohup) so they keep running after this shell
# / the chat closes, as long as WSL stays up. Logs -> .local/dry-*.log, PIDs ->
# .local/dry.pids. Stop with scripts/dry-stop.sh.
set -uo pipefail
cd "$(dirname "$0")/.."
[ -f .env ] && { set -a; . ./.env; set +a; }
mkdir -p .local
NATS_URL="nats://localhost:4222"
PIDS=.local/dry.pids

: "${TELEGRAM_BOT_TOKEN:?set TELEGRAM_BOT_TOKEN in .env}"
: "${TELEGRAM_CHAT_ID:?set TELEGRAM_CHAT_ID in .env}"

echo ">> building"; make build >/dev/null && go build -o bin/natsd ./tools/natsd

# Clear any prior dry-run so ports are free, then start fresh.
bash scripts/dry-stop.sh >/dev/null 2>&1 || true
sleep 1
: > "$PIDS"

start() { # name, "ENV=val ...", binary
  local name="$1" envs="$2" bin="$3"
  env $envs nohup "$bin" >".local/dry-$name.log" 2>&1 &
  echo "$!" >> "$PIDS"
  echo ">> $name (pid $!)"
}

start natsd "" ./bin/natsd
sleep 1
start leaderboard "LEADERBOARD_GRPC_ADDR=:50051 LEADERBOARD_DB_PATH=.local/dry-lb.db NATS_URL=$NATS_URL" ./bin/leaderboard
start enrichment "ENRICHMENT_GRPC_ADDR=:50052 ENRICHMENT_DB_PATH=.local/dry-enr.db" ./bin/enrichment
sleep 2
# strategy is intentionally NOT started: it produces the mirror-proposal /
# approve-reject messages, which are only useful for live trading. A dry run is
# alerts-only, so we skip it to keep one clean message per trade.
start consensus "CONSENSUS_DB_PATH=.local/dry-consensus.db NATS_URL=$NATS_URL" ./bin/consensus
start pricewatch "NATS_URL=$NATS_URL PRICEWATCH_DB_PATH=.local/dry-pricewatch.db PRICEWATCH_POLL_INTERVAL=2m" ./bin/pricewatch
start notifier "TELEGRAM_BOT_TOKEN=$TELEGRAM_BOT_TOKEN TELEGRAM_CHAT_ID=$TELEGRAM_CHAT_ID ENRICHMENT_GRPC_ADDR=localhost:50052 LEADERBOARD_GRPC_ADDR=localhost:50051 NATS_URL=$NATS_URL" ./bin/notifier

if [ -n "${RPC_WSS_URL:-}" ]; then
  start watcher "RPC_WSS_URL=$RPC_WSS_URL LEADERBOARD_GRPC_ADDR=localhost:50051 NATS_URL=$NATS_URL WATCHER_DB_PATH=.local/dry-watcher.db" ./bin/watcher
else
  echo "!! watcher SKIPPED — set RPC_WSS_URL in .env for live whale trades"
fi

echo ">> trader: NOT started (dry run — nothing executes)"
echo ">> dry run live. logs: .local/dry-*.log   stop: make dry-stop   alerts -> your Telegram"
