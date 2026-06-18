# Running locally (read + alert + approve, no execution)

This runs the full pipeline **except order placement** — safe from the US, since
everything here is public market data plus your own Telegram bot. The trader is
not started, so nothing is ever executed.

## 1. Get the two inputs

- **Telegram bot** (for alerts on your phone):
  1. In Telegram, message **@BotFather** → `/newbot` → copy the **bot token**.
  2. Message your new bot once, then open
     `https://api.telegram.org/bot<TOKEN>/getUpdates` and copy your numeric
     **chat id** (`message.chat.id`).
- **Polygon WSS** (for live whale trades): a free Alchemy/Infura **WebSocket**
  Polygon endpoint (`wss://...`). Optional — without it the watcher is skipped
  and you can still test alerts with `make inject-trade`.

## 2. Configure

```bash
cp .env.example .env
# edit .env:
#   TELEGRAM_BOT_TOKEN=...
#   TELEGRAM_CHAT_ID=...
#   RPC_WSS_URL=wss://polygon-mainnet.g.alchemy.com/v2/...   # optional
```

## 3. Run

```bash
make run-local
```

This starts (no Docker needed): an embedded NATS, leaderboard, enrichment,
strategy, and — if the env is set — notifier and watcher. Ctrl-C stops everything.

## 4. Test the alert path without waiting for a whale

In another terminal (while `run-local` is up):

```bash
make inject-trade                              # default buy 5.76 @ 0.95
make inject-trade ARGS="-side sell -size 12 -price 0.42"
```

A `trades.detected` is published → strategy vets it → if it proposes, notifier
DMs you the proposal with ✅/❌ buttons. Tapping ✅ publishes `orders.approved`
(which only the trader would act on — and the trader isn't running).

## What's NOT running

`trader-svc` (the sole key holder) is intentionally left out. Live order
placement requires a permitted jurisdiction (Polymarket geofences US persons)
and the proxy-wallet setup in `docs/TRADER-SETUP.md`.
