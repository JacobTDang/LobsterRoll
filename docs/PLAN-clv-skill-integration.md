# Design Plan: Phase 3c — CLV → skill integration

Status: **plan only — not implemented.** Folds Closing Line Value (the lowest-variance skill
signal) into the pipeline now that pricewatch captures closes (3a/3b done). Builds on
[[polymarket-v2-migration]] data facts and the methodology in `docs/PLAN-roi-skill-sizing.md`.

## The constraint that shapes everything

CLV per trade = compare the **entry price** to the market's price **near close**. Computing it
needs three things joined:
1. the trade's entry (price, side, tokenId) — from `trades.detected` (and `/activity`),
2. the market's **resolution time** — from gamma `endDate` (enrichment),
3. a **captured close snapshot** — from pricewatch (`Nearest(token, endDate−buffer)`).

**Closes only exist for markets pricewatch watched**, and pricewatch only watches markets that
**tracked wallets traded**. So CLV is structurally limited to the *tracked universe*:
- ✅ It can **confirm / re-rank / retain** wallets we already track ("is this whale really beating
  the close, or just lucky on ROI?").
- ✅ It strengthens over time as observations accumulate (it cannot be backfilled — that's *why*
  pricewatch exists).
- ❌ It **cannot promote a brand-new candidate** the day it appears (no observed closes yet).

Therefore: **primary ranking stays shrunk ROI** (computed from full `/activity` history for the
whole candidate pool); **CLV is a confidence-weighted refinement** applied to wallets that have
enough observed CLV samples. This is the honest, correct role — not a universal ranking key.

## Architecture decision: pricewatch owns CLV; leaderboard consumes one call

pricewatch is the natural CLV computer — it already (a) sees `trades.detected` (entry price/side/
token for tracked wallets) and (b) holds the close snapshots. It needs only the resolution time,
so it gains **one** dependency (an enrichment gRPC client for `endDate`, cached per token).

Rejected alternative — leaderboard computes CLV by reading pricewatch close prices: it would need
`Activity.Price`/`Asset` parsing **plus** an enrichment dep **plus** per-trade gRPC chatter, and
still hits the same close-availability limit. Centralizing in pricewatch is less surface and one
clean per-wallet gRPC call for the leaderboard.

Resolution-time source: use enrichment `endDate` (authoritative). A no-dependency alternative —
detect resolution when a token's mid degenerates to ~0/1 and take the prior snapshot — is fiddly
(legit prices reach 0.97) and post-resolution `/midpoint` behavior is uncertain; prefer endDate.

## Data + flow (in pricewatch)

New table `clv_trades`:
```
clv_trades(wallet TEXT, token_id TEXT, tx TEXT, log_index INT,
           entry REAL, buy INT, observed_unix INT,
           clv REAL, settled INT,           -- clv computed once settled=1
           PRIMARY KEY (tx, log_index, wallet))
```
- **Record:** on each `trades.detected`, insert (wallet, token, entry=price, buy, ts). Dedup by
  (tx,logIndex,wallet) — reuse the watcher's at-least-once key shape.
- **Settle (background job, e.g. hourly):** for unsettled trades whose token's `endDate` (enrichment,
  cached) is in the past, look up `close = store.Nearest(token, endDate − closeBuffer)`; if a
  snapshot exists, `clv = clv.CLV(entry, close, buy)`, set `settled=1`. closeBuffer ≈ 4h (a
  late-but-liquid point; configurable). Trades whose market never got a snapshot stay unsettled and
  simply don't contribute (graceful sparsity).
- **Aggregate:** per wallet over settled rows → `avgCLV`, `beatRate` (% clv>0), `n`.
- **Prune:** drop settled rows older than retention.

## gRPC surface (new pricewatch server)

pricewatch gains a gRPC server (it has none today):
```proto
service Pricewatch {
  rpc GetWalletCLV(GetWalletCLVRequest) returns (WalletCLVBatch); // batch by wallet list
}
message WalletCLV { string wallet; double avg_clv; double beat_rate; int64 n; }
```
Leaderboard dials it once per refresh with the candidate wallet list → one round-trip.

## Skill-score integration (leaderboard)

The composite skill score becomes (still non-compensatory):
- Base = shrunk ROI percentile (today's score).
- When a wallet has `n ≥ clvMinSamples` (e.g. 30) observed CLV trades, blend in a CLV component
  (z-scored avg CLV across the population, or a direct bonus), weighted by a confidence factor that
  rises with n. Below the threshold, score = base unchanged (no penalty for being new/unobserved).
- Surface in the alert next to ROI: `📈 ROI +31% · CLV +3.1% (n=142)`.

Folding rule (keep it conservative): `skill = base × (1 + w·clvSignal)` where `w = min(1, n/clvFull)`
and `clvSignal` is the wallet's avg-CLV percentile centered at 0 — so CLV nudges ranking among the
tracked set without dominating, and is inert for unobserved wallets. Exact weighting tuned with data.

## Phasing (each independently verifiable)

- **3c-1 — CLV computation in pricewatch:** `clv_trades` table + record on `trades.detected` +
  enrichment client (endDate, cached) + settle job + per-wallet aggregate. Unit-tested with fakes
  (no live network); the settle logic is pure given (trades, endDate, snapshots).
- **3c-2 — expose + display:** pricewatch gRPC `GetWalletCLV`; leaderboard dials it, stores
  avgCLV/beatRate/n in `wallet_stats`, threads to `WalletStats` proto → notifier shows the CLV line.
  (Display first, like the ROI rollout — high value, low risk.)
- **3c-3 — fold into skill score:** blend CLV into the composite ranking with the confidence weight;
  mutation-verify the blend; document the weighting.

## New config (pricewatch)
`PRICEWATCH_ENRICHMENT_GRPC_ADDR` (dial enrichment), `PRICEWATCH_CLOSE_BUFFER` (4h),
`PRICEWATCH_SETTLE_INTERVAL` (1h), `PRICEWATCH_CLV_RETENTION`. Leaderboard:
`PRICEWATCH_GRPC_ADDR` (dial), `CLV_MIN_SAMPLES`, plus the blend weight knob.

## Honest caveats
- CLV is **forward-accumulating + tracked-universe-only** — sparse at first, never backfilled. The
  blend must be inert below the sample threshold so it never penalizes unobserved wallets.
- `endDate` can be rescheduled; re-read it (enrichment cache TTL already handles staleness).
- `closeBuffer` is a judgment call (prediction markets have favorite-longshot bias near close) —
  start at 4h, tune against realized ROI once data exists.
- Adds an enrichment dep to pricewatch and a pricewatch gRPC dep to the leaderboard — both are
  internal, US-safe, read-only.

## Verification approach
Pure settle/aggregate logic and the blend are fully unit-testable with fakes (trades + endDates +
snapshots in, CLV/score out) — no live network, mutation-verified. The live capture→settle path is
exercised end-to-end only in a deployed dry run; document a `make verify-clv` analog that injects a
trade + a snapshot + a past endDate and asserts the computed CLV.
