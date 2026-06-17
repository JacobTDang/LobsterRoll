# LobsterRoll

Copy-trade the top Polymarket wallets. LobsterRoll watches the leaderboard's wallets,
detects their trades on-chain in near-real-time, alerts you on Telegram, and — with your
approval — mirrors the trade from your own Polymarket account.

> ⚠️ **Handles real funds.** The trader service signs orders with a private key. Use a
> **dedicated wallet** funded with a capped amount. See [PLAN.md](PLAN.md) phase 7.

## Architecture

Six Go microservices, gRPC for sync contracts, NATS for the async event pipeline,
deployed on **k3s** (Raspberry Pi / arm64).

```
 data-api ─► leaderboard-svc ──(gRPC watchset)──┐
                                                ▼
 Polygon WSS ─► watcher-svc ──trades.detected──► strategy-svc ──orders.proposed──► notifier-svc ◄─► Telegram
   (OrderFilled        │ (gRPC EnrichToken)                                              │ approve
    on CTF V1+V2)      ▼                                                    orders.approved▼
                 enrichment-svc                                                    trader-svc ──► Polymarket CLOB
                                                                          (sole key holder, hard caps, /halt)
```

| Service | Key? | Role |
|---------|------|------|
| leaderboard | no | top-N proxy wallets → watchset (gRPC) |
| watcher | no | WSS `OrderFilled` decode/filter + gap-backfill |
| enrichment | no | tokenId → market/outcome (cached, gRPC) |
| strategy | no | copy decision, sizing, slippage/risk checks |
| trader | **yes** | signs + places orders; independent hard caps |
| notifier | no | two-way Telegram (alerts, approve/reject, `/halt`) |

## Stack
Go · gRPC (buf) · NATS · go-ethereum · go-order-utils (EIP-712) · pure-Go SQLite
(`modernc.org/sqlite`, CGO-free) · Docker (`ubuntu:24.04`) · k3s / k3d.

## Quickstart (WSL Ubuntu)
```bash
# one-time toolchain (Go + buf + golangci-lint, no sudo)
bash scripts/wsl-setup.sh && source ~/.bashrc

cp .env.example .env        # fill in as phases require keys
make test                   # unit tests (-race via `make test-race`)
make proto                  # regenerate gRPC stubs into gen/
make build                  # build all six services into bin/
```

## Layout
```
proto/       gRPC contracts (source of truth)
pkg/         shared libs: svc, bus, config, chain
services/    one main.go per microservice
deploy/k8s/  k3s manifests   deploy/k3d/  local cluster
scripts/     dev bootstrap
```

## Roadmap
Built in phases — see **[PLAN.md](PLAN.md)** for the full plan and the tests gating each
phase. API keys are introduced only when their phase needs them (Telegram → phase 4,
RPC → phase 2, wallet key → phase 7).
```
phase 0  foundation (this commit)
phase 1  leaderboard-svc      phase 6  notifier two-way (approval)
phase 2  watcher-svc          phase 7  trader-svc (execution)
phase 3  enrichment-svc       phase 8  k3s orchestration
phase 4  notifier (alerts)    phase 9  hardening → auto
phase 5  strategy-svc
```
