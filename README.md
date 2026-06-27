# LobsterRoll

Copy-trade the best Polymarket wallets. LobsterRoll ranks the leaderboard's
proven performers, detects their trades on-chain in near-real-time, enriches and
scores each signal, alerts you on Telegram, and — once your account is eligible —
mirrors the trade from your own wallet behind a hard-capped safety net.

The read-and-alert pipeline is pure public data and runs anywhere. Execution is a
separate, opt-in service that ships **disabled** and is gated behind exchange
eligibility and an explicit configuration step.

---

## How it works

```
  data-api / lb-api ──► leaderboard ──┐  gRPC: watchset + wallet stats
                                      │   (shrunk-ROI · freshness · CLV)
                                      ▼
  Polygon WSS ──► watcher ──trades.detected──► consensus ──┐
   (OrderFilled CTF V1+V2)     │                           ├──► notifier ◄──► Telegram
                               │   enrichment (token→market)│     (alerts · approve · /halt)
                               ▼                            │
                           strategy ──proposal (sizing)─────┘
                               │
                               └── approved ──► trader ──► Polymarket CLOB
                                               (sole key holder · hard caps · /halt)

  pricewatch ──► captures closing-line value (CLV) ──► leaderboard (skill signal)
```

A watched wallet's fill is decoded on-chain, resolved to a human-readable market,
scored against the wallet's track record, and pushed to Telegram. When several
tracked wallets converge on the same outcome it fires a premium consensus alert.
Nothing is executed without an eligible, explicitly-enabled trader.

## Services

Eight Go microservices: gRPC for synchronous contracts, NATS for the asynchronous
event pipeline, each with its own SQLite state where needed.

| Service     | Holds key | Role |
|-------------|-----------|------|
| leaderboard | no  | Ranks top wallets into a watchset and serves their stats over gRPC |
| watcher     | no  | Subscribes to Polygon `OrderFilled` (CTF V1 + V2), decodes/filters watched wallets, backfills gaps |
| enrichment  | no  | Resolves a tokenId to market / outcome / end-date (cached) |
| consensus   | no  | Fires when multiple tracked wallets converge on the same outcome |
| pricewatch  | no  | Captures closing-line value (CLV) to feed the skill ranking |
| strategy    | no  | Copy decision and position sizing (fractional Kelly, spread- and depth-aware) |
| notifier    | no  | Two-way Telegram: trade alerts, position-exit alerts, approve / reject, `/halt` |
| trader      | yes | Signs and places orders behind independent hard caps and a halt switch (ships disabled) |

## The edge

Selection is built to favor demonstrated skill over a lucky streak:

- **Shrunk ROI** — empirical-Bayes credibility shrinkage pulls small-sample ROI
  toward the population mean, so a few hot bets cannot out-rank a long record.
- **Freshness** — a one-sided CUSUM on each wallet's return series flags wallets
  that have started cooling off, so the watchset tracks who is hot *now*.
- **Closing-line value** — pricewatch records where the market settled relative to
  the wallet's entry, the lowest-variance measure of genuine edge.
- **Consensus** — independent convergence by multiple proven wallets is a stronger
  signal than any single trade.
- **Sizing** — fractional Kelly bounded by spread, order-book depth, exposure caps
  and a drawdown de-risk, so position size follows conviction and liquidity.

## Stack

Go 1.25 · gRPC (buf) · NATS · go-ethereum · go-order-utils (EIP-712) ·
`modernc.org/sqlite` (CGO-free) · Prometheus metrics · ko / k3s / k9s on WSL.

## Quickstart

```bash
# one-time toolchain (Go + buf + golangci-lint, no sudo)
bash scripts/wsl-setup.sh && source ~/.bashrc

cp .env.example .env   # fill in keys as each phase needs them
make proto             # regenerate gRPC stubs into gen/
make build             # build every service into bin/
make test              # unit tests  (race: make test-race)
```

## Running

```bash
make dry-run        # detached read-and-alert pipeline (no execution) -> Telegram
make dry-logs       # tail the running services
make dry-stop       # stop the dry run

make verify-sizing  # run sample signals through the sizing engine and print stakes
make verify-clv     # drive the CLV pipeline end-to-end and assert the value
```

Keys are introduced only when a phase needs them: an RPC WebSocket URL for the
watcher, a Telegram bot token for the notifier, and — for execution only — a
dedicated, capped wallet key for the trader.

## Layout

```
proto/        gRPC contracts (source of truth)
pkg/          shared libraries: svc, bus, config, chain, dataapi, dedup,
              httpx, metrics, sizing, exit, sqlitex
services/     one main.go per microservice
deploy/k8s/   k3s manifests (trader ships at replicas: 0)
scripts/      dev bootstrap, image build, dry-run
docs/         design notes and phase plans
```

## Status

The read, rank, enrich, score, consensus and alert pipeline is complete and runs
end-to-end. Position sizing and the exit/hedge engine are implemented and tested
as pure logic; the trader is wired but ships disabled, gated behind exchange
eligibility. Position-exit alerts are built and activate once a public wallet
address is configured.

See [PLAN.md](PLAN.md) for the phased build and the tests gating each phase, and
[docs/](docs/) for the design notes behind ranking, CLV, sizing and exits.

## Safety

The trader is the only component that holds a private key and the only one that
moves funds. It runs behind hard caps (per-trade, daily, open-exposure), a kill
switch (`/halt`), and an approval gate, and it is disabled by default. Use a
dedicated wallet funded with a capped amount. Secrets live in `.env` (gitignored)
or a Kubernetes Secret and are never committed.
