# LobsterRoll — Multi-Phase Build Plan

Copy-trade the top Polymarket wallets: watch their on-chain trades in near-real-time,
notify via Telegram, and (with approval) mirror the trade from your own account.

## Engineering principles
- **Concurrency-first (Go):** goroutines + channels, `context` cancellation, `errgroup`
  for fan-out/fan-in, bounded worker pools. Every binary uses `pkg/svc.Run` for uniform
  graceful shutdown.
- **Test-driven:** write the test first. Keep logic in pure, table-tested functions
  (parsers, decoders, sizing, caps) separated from I/O. Always run `go test -race`.
- **Efficiency:** CGO-free (`modernc.org/sqlite`) for clean arm64 cross-builds; cache
  enrichment; avoid allocations in the watcher hot path.

## Test strategy (applies to every phase)
| Layer | Tooling | Runs in |
|-------|---------|---------|
| Unit (default) | stdlib `testing`, table-driven, golden files | CI, always, `-race` |
| gRPC | `google.golang.org/grpc/test/bufconn` (in-memory) | CI |
| HTTP clients | `net/http/httptest` mock servers | CI |
| Bus | embedded `nats-server` (`test` helper) | CI |
| Integration | real APIs, gated by `//go:build integration` + env (keys/RPC) | manual / on-demand |
| E2E | k3d cluster smoke test | manual / nightly |

Coverage target: **>80%** on `pkg/`, `strategy`, and the trader risk-caps. Integration
tests never run by default (no secrets in CI).

---

## Phase 0 — Foundation *(this commit)*
**Goal:** compiling, tested monorepo skeleton; nothing talks to the network yet.

**Deliverables:** module layout, `proto/` contracts, shared `pkg/` (`svc`, `bus`,
`config`, `chain`), six service stubs, Dockerfile (multi-stage → `ubuntu:24.04`),
k8s/k3d manifests, CI, `scripts/wsl-setup.sh`.

**Tests / verification:**
- `go test ./...` green: `ParseExecutionMode` + `RequiresApproval` (table), `AllSubjects`
  uniqueness, `WatchedExchanges` shape.
- `go vet ./...`, `buf lint`, `go build ./...` for all six services.
- **Exit criteria:** CI passes on `main`.

**API keys needed:** none.

---

## Phase 1 — leaderboard-svc (the watchset)
**Goal:** maintain the top-N proxy wallets from Polymarket's leaderboard; serve them over gRPC.

**Deliverables:** `data-api` leaderboard client; address normalization; SQLite watchset
store; periodic sync (ticker goroutine); gRPC `GetWatchset` + `StreamWatchset(added/removed)`.

**Tests:**
- *Unit:* leaderboard JSON parsing from golden fixtures; top-N selection; address
  normalization/dedup; watchset diff (added vs removed) logic.
- *Store:* CRUD against a temp SQLite file; diff emission on change.
- *gRPC:* bufconn server test for both RPCs; stream emits on watchset change.
- *Integration (gated):* hit real `data-api`, assert non-empty + schema shape.
- **Exit:** `grpcurl GetWatchset` returns N wallets; stream pushes a diff when N changes.

**API keys needed:** none (public API).

---

## Phase 2 — watcher-svc (the hard part)
**Goal:** detect watched wallets' trades on-chain in <5s and publish them.

**Deliverables:** go-ethereum `ethclient` WSS subscription to `OrderFilled` on CTF
Exchange **V1 + V2**; log decode → `Trade`; filter by watchset (gRPC client + live
`StreamWatchset`); persist `last_processed_block`; **gap-backfill** via `FilterLogs` on
startup/reconnect; dedup by `(txHash, logIndex)`; reconnect with backoff; publish
`trades.detected` to NATS. Concurrency: subscription goroutine + decode worker pool +
backfill goroutine under one `errgroup`.

**Tests:**
- *Unit (critical):* `OrderFilled` ABI decode from **real raw-log golden fixtures** →
  exact `Trade` fields (wallet, tokenId, side, price, size).
- *Unit:* maker/taker → side mapping; dedup seen-set; backoff schedule (table); watchset
  filter (only tracked wallets pass).
- *Backfill:* given `lastBlock` + simulated logs, assert no gaps and no duplicates vs live.
- *Integration (gated, needs `RPC_WSS_URL`):* connect to real Polygon WSS, decode ≥1 real
  `OrderFilled` within a timeout.
- **Exit:** point at real RPC + a known active wallet → `trades.detected` on NATS within 5s.

**API keys needed:** **Polygon RPC WSS URL** (free public to start; Alchemy/Infura free tier fallback).

---

## Phase 3 — enrichment-svc (human-readable markets)
**Goal:** turn a tokenId into "market question / outcome".

**Deliverables:** gamma/clob client; gRPC `EnrichToken`; SQLite cache; concurrency-safe.

**Tests:**
- *Unit:* gamma response parsing (golden); cache hit/miss; **race test** on concurrent cache access.
- *gRPC:* bufconn `EnrichToken`.
- *Integration (gated):* resolve a known tokenId → correct market question.
- **Exit:** enrich a real tokenId correctly; second call served from cache.

**API keys needed:** none.

---

## Phase 4 — notifier-svc (one-way alerts)
**Goal:** Telegram message on every detected trade (enriched).

**Deliverables:** Telegram `sendMessage` client; consume `trades.detected`; call
enrichment; format buy/sell messages.

**Tests:**
- *Unit:* message formatting golden strings (buy/sell, size/price formatting).
- *Client:* `httptest` mock asserting correct Telegram API payload.
- *Handler:* consume-from-bus unit test (embedded NATS).
- *Manual:* real bot token → message lands on your phone.
- **Exit:** a real whale trade produces a Telegram alert end-to-end.

**API keys needed:** **Telegram bot token + chat id.** *(First keys you'll provide.)*

---

## Phase 5 — strategy-svc (decision + risk, no execution)
**Goal:** turn a detected trade into a vetted order proposal — or skip it.

**Deliverables:** consume `trades.detected`; sizing policy (fixed/proportional);
**max-slippage guard** (skip if price moved past threshold); market filters (allowlist,
min liquidity, min size); emit `orders.proposed`. Pure, heavily-tested functions.

**Tests:**
- *Unit:* sizing math (table); slippage guard (skip when beyond N cents); filters; proposal
  generation golden; idempotency (one proposal per source trade).
- *Handler:* synthetic trades in → expected proposals/skips out (embedded NATS).
- **Exit:** replay a fixture trade stream → correct proposals and skips.

**API keys needed:** none.

---

## Phase 6 — notifier-svc two-way (approval gate)
**Goal:** approve/reject proposals from Telegram; kill switch.

**Deliverables:** inline ✅/❌ buttons on proposals; callback handling →
`orders.approved` / `orders.rejected`; `/halt` and `/resume` commands → `control.halt`.

**Tests:**
- *Unit:* callback payload parse; idempotent approval (can't approve twice); halt state machine.
- *Client:* `httptest` for callback answer + message edit.
- *Manual:* receive a proposal, tap ✅ → `orders.approved` observed.
- **Exit:** full approval round-trip works; `/halt` blocks downstream.

**API keys needed:** Telegram (already provided in phase 4).

---

## Phase 7 — trader-svc (execution) ⚠️ real money
**Goal:** sign and place the approved order on Polymarket; enforce hard caps.

**Deliverables:** L1→L2 auth (derive creds); EIP-712 order signing via **`go-order-utils`**;
CLOB place-order; consume `orders.approved`; **independent hard caps** (per-trade / per-day /
open-exposure) enforced regardless of strategy; honor `control.halt`; idempotent placement;
`EXECUTION_MODE` routing (approval / auto / auto_below); publish `orders.filled` / `orders.failed`.

**Tests:**
- *Unit (critical):* deterministic EIP-712 signature for a fixed order+key (**golden vector**);
  L2 HMAC header generation (golden).
- *Caps (critical):* reject over per-trade / per-day / exposure limits (table); daily reset;
  **race test** on concurrent cap accounting.
- *Halt:* refuses to place when halted.
- *Client:* `httptest` CLOB mock asserting signed payload + headers; simulate accept / reject / partial-fill.
- *Idempotency:* same proposal never placed twice.
- *Integration (manual, tiny real order):* place a ~$1 order on a real market from the
  **dedicated capped wallet**, assert fill, then cancel.
- **Exit:** approved proposal → real small fill on the dedicated wallet; caps + halt proven.

**API keys needed:** **Polymarket API key/secret/passphrase + the dedicated wallet's private
key** (injected via k8s Secret only). Provide last, after caps/halt tests pass.

---

## Phase 8 — Orchestration on k3s
**Goal:** the whole system self-heals on the Pi.

**Deliverables:** Deployment/Service/PVC for all six services; `/healthz` +
liveness/readiness probes; resource limits; NATS JetStream; multi-arch `buildx` → registry →
deploy to Pi k3s; k3d for local e2e.

**Tests:**
- *Unit:* `/healthz` handler per service.
- *Manifests:* `kubeconform` / `kustomize build` in CI.
- *E2E (k3d):* bring up stack, inject a synthetic trade (mock Telegram + CLOB), assert the
  proposal→notify path.
- *Chaos:* `kubectl delete pod watcher` → it restarts and **resumes from `last_processed_block`**
  with no missed/duplicate trades.
- **Exit:** deploy to Pi; kill a pod and watch it recover; real trade → notification.

**API keys needed:** all of the above, as k8s Secrets.

---

## Phase 9 — Hardening, then go automatic
**Goal:** observability + soak, then flip approval → auto.

**Deliverables:** Prometheus metrics, structured logs, alert on watcher disconnect /
dead-man's switch, dashboards. Soak in approval mode, then
`EXECUTION_MODE=auto_below:<$>` → `auto`.

**Tests:**
- *Unit:* metrics presence; disconnect alert fires.
- *Soak:* long-running stability; reconciliation correctness across a restart storm.
- **Exit:** clean soak with zero missed trades; switch to auto with confidence.

**API keys needed:** none new.

---

## Critical-path test summary
The bugs that cost money or trades live in three places — these get the deepest coverage:
1. **`OrderFilled` decode** (phase 2) — golden-fixture exactness.
2. **Trader hard caps + halt** (phase 7) — table + race tests; the last safety net in auto mode.
3. **Backfill/dedup** (phase 2/8) — no missed or double trades across restarts/reconnects.
