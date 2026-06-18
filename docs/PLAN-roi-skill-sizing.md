# Design Plan: ROI-led signal quality, skill/decay scoring, and position sizing

Status: **plan only — not implemented.** Synthesized from three research passes (Polymarket
data APIs, open-source landscape, skill/sizing methodology). Every load-bearing formula was
verified against primary sources (Kelly 1956, Thorp 2006, Lo 2002, Buchdahl on CLV, Gneiting &
Raftery, NIST CUSUM). Citations live in the research transcripts; key ones inlined below.

## 0. Framing & the two hard gates

**Product direction:** signal-quality first (works today, read-only, US-safe) → automated sizing &
execution once an eligible Polymarket account is connected. The edge we are selecting/displaying on
is **ROI / Closing-Line-Value, not win rate** — a 90%-win wallet buying 0.95 favorites can be
break-even or negative EV.

Two prerequisites gate the *execution* half (not the analytics half):

- **GATE 1 — v2 contracts.** Polymarket migrated to **CTF Exchange V2 on 2026-04-28**; both official
  CLOB clients were **archived 2026-05-25** and replaced with `-v2` repos with new signing. Our
  hand-rolled EIP-712 signer and the watcher's `OrderFilled` decode **must be verified against v2**
  before any order is placed. (The dry run still catches live trades, so the watcher is likely fine,
  but verify explicitly.) See [[polymarket-orderfilled-abi]], [[polymarket-clob-order-signing]].
- **GATE 2 — US eligibility.** Polymarket geofences US persons from *trading*. Read APIs work from
  the US (the dry run proves it); order placement does not. Phase B/C ship the jurisdiction-neutral
  *logic*, but actual execution against Polymarket requires a path you are eligible for — that is a
  compliance decision, not something this plan engineers around the geoblock.

---

## PHASE A — ROI + skill/decay engine (now; read-only; US-safe)

Closes the PM gaps: "win-rate ≠ profit", "skilled vs cooling off", "ROI in the message".

### A1. ROI metric (into the alert + selection)

**Data (verified available):** `data-api /positions` already returns per-position `realizedPnl`,
`percentPnl`, `percentRealizedPnl`, `avgPrice`, `totalBought`, `cashPnl`, `initialValue`,
`currentValue`. We do **not** need to reconstruct cost basis from scratch for open positions.
For lifetime/closed ROI, aggregate `/activity` (sum `usdcSize` of BUY/SPLIT as capital deployed;
net BUY−/SELL+/REDEEM+/MERGE+/SPLIT− for realized profit). `/activity` paginates at 500, offset
cap 10000 → time-window the paging for large wallets.

**Definition:** ROI = realized profit / capital deployed (resolved positions only — never count
open paper gains). Compute **per-bet returns** `r_i` in fractional units (`r_i = o−1` on win,
`−1` on loss) so the metric is bankroll-independent and feeds the stats below.

**Confidence:** report ROI with a **bootstrap 95% CI** (per-bet returns are bounded at −1 with a
long right tail, so the plain t-interval mis-covers). Fast screen: `t = ROI·√n / sd` (this is
identical to `Sharpe·√n`). Sample-size reality: <100 bets meaningless, 300–500 = useful floor,
1000+ = meaningful, 2000+ = pro-grade — gate accordingly.

**Surface in the alert (the explicit ask):**
```
🟢 ENTER (BUY)  whale 0x37e4…c991   SKILL 87 ✅ fresh
Ghana vs. Panama: O/U 2.5 → Over
📈 ROI +31%  (shrunk +22%, 95% CI +9…+35%)  ·  142 mkts  ·  CLV +3.1%
👤 78% win · realized +$1.2M · $340k portfolio
💵 $4,200 @ $0.52
🏁 game 2026-06-27 21:00 UTC
```

### A2. Composite skill score (replaces `winRate × log(pnl)` selection)

Score is **non-compensatory** (geometric mean or hard gates) so one weak dimension can't be masked
by a flashy ROI. Components, each z-scored across the **full tracked-wallet population** (not a
pre-filtered top set, or we re-import survivorship bias):

1. **Shrunk ROI** (use the lower CI bound) — see A2a.
2. **Average CLV + beat-close %** — see A4 (the lowest-variance, fastest skill signal; primary).
3. **Calibration:** Brier score `BS = mean((f−o)²)` and skill score `BSS = 1 − BS/UNC`; weight by
   market liquidity (thin markets are noisy — Polymarket Brier ≈0.03 in deep, ≈0.19 in thin).
4. **Risk-adjusted:** Sharpe `mean/sd` of per-bet returns; use **PSR(0)** when skewed; gate on n≥30.
5. (display-only) max drawdown + longest-loss-streak vs expected.

**A2a. Shrinkage (fights regression-to-the-mean / luck).**
- Win-rate (Beta-Binomial): `eb = (α+wins)/(α+β+n)`, prior `α,β` from population by method-of-moments.
- ROI (Normal-Normal): `roi_est = grand_mean + w·(roi_obs − grand_mean)`, `w = τ²/(τ² + σ²/n)`,
  `τ² = Var(observed ROIs) − mean(σ²/nᵢ)` (clamp ≥0). Small-n wallets pulled hard to the mean.
- **Rank by the shrunk estimate (or its lower bound), never raw ROI.**

Gate: zero the composite below ~300–500 resolved bets; always show component metrics; version the
weights. This **replaces the current strict gates** (≥90% win / ≥$100k / ≥20 resolved) — keep
portfolio ≥ threshold as a hard filter, but drive *ranking* by skill, not win rate.

### A3. Freshness / cooling-off flag (separate, traffic-light)

Answers "is it still good *now?*" independent of how good it was. **RED** when any fire:
- **Two-sided CUSUM** on z-scored returns, `k=0.5, h=5` (catches a 1σ break in ~10 bets,
  false-alarm interval ARL₀≈465): `S_lo = max(0, S_lo − z − k)`, alarm when `S_lo > h`.
- **EWMA** of returns below threshold (RiskMetrics `EWMA_t = λ·EWMA_{t−1} + (1−λ)x_t`; tune half-life).
- Observed losing streak z-score > 3 vs expected `ln(n·p)/L` (L=ln(1/q)).
- Recent-vs-prior rolling-window Welch test significantly negative.
- **CLV trend** turning negative — the earliest warning (leads ROI).

**Select only when skill is high AND freshness is green.** This is the direct guard against copying
a decayed-but-historically-great wallet.

### A4. CLV capture service (NEW component — required by a real API constraint)

**Why new:** `/prices-history` only returns ≥12h granularity on *resolved* markets (verified —
py-clob-client issue #216), so a closing line **cannot be reliably backfilled after the fact**. We
must **capture prices live** while markets are active.

- New svc **`pricewatch`**: subscribes to the public market WS
  `wss://ws-subscriptions-clob.polymarket.com/ws/market` (no auth) for the tokens our tracked
  wallets hold/trade (drive its subscription set from the watchset + observed trades). Persists
  periodic price snapshots `(tokenId, ts, midprice)` to SQLite (single-writer via `pkg/sqlitex`).
- CLV per copied/observed bet = `entry_price` vs a **T-minus-X snapshot** (use a late-but-liquid
  point, T-4h…T-1d, with a liquidity floor; devig the two-sided YES/NO quote first:
  `p_i = (1/oᵢ)/Σ(1/oⱼ)`). Prediction markets have favorite-longshot bias → compare against the
  bias-aware near-close price, not resolution (0/1).
- Reuses the watcher's WS reconnect/staleness discipline (the WS is known to silently freeze —
  py-clob-client #292 — so add a watchdog).

### A5. Per-category skill (gamma `tags`/`category`)

A wallet 90% in sports may be a coin-flip in politics. Bucket each wallet's bets by gamma
`category`/`tags`; compute skill per category; the alert/score uses the **category-specific** skill
for the market in question, with a fallback to overall when sample is thin.

### Phase A footprint
- **leaderboard svc:** extend `stats.Compute` → ROI + per-bet returns + Brier + Sharpe + drawdown;
  new `skill` package (shrinkage, composite, freshness/CUSUM/EWMA); selection ranks by skill+fresh.
- **pricewatch svc (new):** live price capture for CLV.
- **enrichment:** already returns category via gamma (extend response with `tags` if needed).
- **notifier `format`:** new ROI/CLV/skill/freshness lines; **proto** `WalletStats` gains
  `roi`, `roi_shrunk`, `roi_ci_low/high`, `clv_avg`, `brier`, `skill_score`, `fresh` (bool).
- **New SQLite tables:** `price_snapshots`, `wallet_skill` (cached scores + components, versioned).
- All read-only → **ships in the US, no account, no v2 dependency.**

---

## PHASE B — Sizing engine (scaffold now; activate on account-connect + GATE 1/2)

Turns a signal into a concrete stake. Jurisdiction-neutral logic; wired to the existing
`strategy → trader` path (strategy proposes a *sized* order; trader executes).

### B1. Infer edge when copying (we never see the leader's true probability)
Build edge from two noisy reads, then **shrink hard toward the market price** (a defensible prior:
deep Polymarket markets are ~84% accurate / Brier ≈0.084):
- **Track-record edge (primary):** `q_track = p_mkt_novig + realized_CLV%`. CLV is the
  lowest-variance input — prefer it over ROI inversion `q = (ROI+1)/d`.
- **Conviction edge (noisiest — shrink most):** normalize the leader's stake to **their own
  historical stake-size percentile** (controls for unknown bankroll/risk pref) rather than guessing
  absolute bankroll; invert Kelly `q = (f·(d−1)+1)/d`.
- **Bayesian shrinkage to market prior** (Beta-Binomial, prior mean `p_mkt`, strength 20–100, the
  copied bet ≈ n=1) → the estimate barely leaves `p_mkt`, which is correct for a single
  high-variance observation. (Bayesian-Kelly cuts drawdown 40–60% vs full Kelly at 85–95% of growth.)

### B2. Kelly, fractionalized, cost-aware
- `f* = (q − p)/(1 − p)` (binary/prediction-market form; bet only if `f* > 0`).
- **Fractional Kelly k = 0.25 → 0.5** (start quarter-Kelly; half-Kelly keeps 0.75 of growth at half
  the volatility; full Kelly ≈50% chance of ever halving vs ~12% at half). Overbetting is far more
  harmful than underbetting and edge estimates skew optimistic.
- **Effective edge after costs:** `edge_eff = edge_shrunk − half_spread − fees − slippage`.
  Polymarket taker fee = `shares·feeRate·p·(1−p)` (peaks at p=0.5; feeRate 0–0.07). **Skip** if
  `edge_eff ≤ 0`, below a 2–3% buffer, or spread > ~1–2¢.
- **Inputs from CLOB:** `/book` (full L2 depth), `/spread`, `/midpoint`, `/price` (+ batch + WS).
  Compute **microprice** `(bid·askSz + ask·bidSz)/(bidSz+askSz)`; walk the ladder for VWAP slippage.

### B3. Bankroll & risk caps
| Constraint | Value |
|---|---|
| Per-bet | ≤2–3% bankroll (hard 5%), on top of fractional Kelly |
| Total simultaneous exposure | 6–10%; a correlated cluster counts as **one** position |
| Market impact | ≤~5% of volume (soft), ≤~20% of book depth (hard) — Polymarket books are thin |
| Drawdown soft de-risk | halve sizing at 5–10% below high-water |
| Drawdown hard stop | full stop / review at 15–20% |

`stake = min(k·f*·bankroll, per_bet_cap·bankroll)`. Correlated same-event markets: size the group as
one Kelly bet (`F* = C⁻¹·(M−R)` general form; for two correlated legs `f ≈ m/(2(2c+m))`). These
extend the trader's existing `caps` package (which already does atomic check-and-commit + `atomic.Bool`
halt + `ON CONFLICT` at-most-once — a solid base).

### B4. Strong prior to respect
Copy-trading empirically *underperforms* and induces over-risk (Apesteguia et al., Mgmt Sci 2020);
~84% of Polymarket traders lose, and only ~12% of top earners beat a luck benchmark. **Therefore
size off the consensus basket (5–10 wallets, ~80%+ agreement) preferentially over any single
wallet** — exactly what our `consensus` service already produces (and which has *no* OSS equivalent —
it's our differentiator).

### Phase B footprint
- **strategy svc:** new `sizing` package (edge inference, Kelly-frac, cost/skip rules), consumes
  skill scores (Phase A) + live book; emits a *sized* `OrderProposal`.
- **trader svc:** verify v2 signing (GATE 1); execute limit orders at/near leader price with a
  max-chase; reconcile fill-vs-sellable (pattern from `tosmart01/polymarket-position-watcher`).
- **New tables:** `bankroll`, `exposure`, per-market/correlation-group tracking.

---

## PHASE C — Position / exit / hedging manager (NEW svc, after B)

The missing other half of every strategy. New `position-manager` svc tracking open copied positions:
- **Scale out in tranches** (e.g. 25% at 2:1, 25% at 3:1, ride the rest; trail stop to breakeven).
  **Never scale out of losers — exit fully.**
- **Hedge to lock profit:** size the opposing stake so outcomes pay equally (`hedge ≈ P/O`); prefer a
  manual hedge over platform cash-out (hidden 5–10% margin). Your example (sell one leg early, hold a
  correlated leg to cash out) = "leg out / green up" — sell the appreciated YES (or buy NO) to spread
  locked profit; unwind one leg of a correlated pair.
- **Whale-exit-driven exits:** integrate the planned [[feature-user-position-exit-alerts]] — when a
  tracked whale exits a market we copied, trigger our exit logic.
- **YES+NO < $1.00** = risk-free arb (rare, fast) — opportunistic.

---

## Cross-cutting — Observability (do alongside Phase A)

Closes the "who watches the watcher" gap and powers the ROI/skill dashboard.
- **`kube-prometheus-stack`** (Helm on k3s, drive in k9s): Prometheus + Alertmanager + Grafana
  (Grafana AGPLv3 — fine for internal self-hosting).
- **`prometheus/client_golang`** in each svc; **nats-io/prometheus-nats-exporter** + the
  go-grpc-middleware Prometheus provider for broker/gRPC health.
- Metrics that matter: `watcher_ws_connected`, `*_reconnects_total`, `signals_emitted_total`,
  `time_since_last_signal` (alarm if no signal in N hours = silent failure), plus a Grafana board for
  per-wallet ROI/skill/freshness and (later) realized P&L (`frser-sqlite-datasource` over our SQLite).

---

## Open-source: adopt / reference / build (verified 2026-06-18)

| Item | Use | Why |
|---|---|---|
| `gonum/gonum` (BSD) | **Adopt** | stats: CIs, Normal/Beta dists, weighted moments |
| `kube-prometheus-stack` (Apache/AGPL) | **Adopt** | observability on k3s |
| `prometheus/client_golang`, nats/grpc exporters (Apache) | **Adopt** | service metrics |
| `frser-sqlite-datasource` (Apache) | **Adopt (caution: solo maint)** | Grafana over our SQLite |
| `py-clob-client-v2` (MIT) | **Reference** | golden-signature oracle to byte-check our Go signer |
| `GoPolymarket/polymarket-go-sdk` (Apache) | **Reference / selective** | v2 EIP-1271 + L2-HMAC patterns |
| `skharchikov/polymarket-bot` (Rust, no license) | **Reference** | closest copy→drift-filter→Kelly twin (ideas only) |
| `tosmart01/polymarket-position-watcher` (MIT) | **Reference** | fill-vs-sellable reconciliation for executor |
| `al1enjesus/polymarket-whales` (MIT) | **Reference** | clean Telegram fan-out patterns |
| SEO-spam `*polymarket-copy-trading-bot*` repos | **Skip** | no license, keyword spam, hazard |

**Build in-house (no good Go dep; ~15–20 lines each, test-first):** Kelly closed-form,
Wilson/Jeffreys CIs, keyed EWMA, CUSUM, order-book microprice/slippage. Keep our hand-rolled Go
EIP-712/CLOB (latency + key control matter on the hot path) — just realign to v2 + add conformance
tests. No free subgraph exists post-v2 → stay on the `data-api` crawl (or paid Goldsky pipelines for
bulk).

---

## Sequencing

1. **Observability** (small, de-risks everything) + **GATE 1 v2 verification** (cheap, unblocks B).
2. **Phase A** (ROI + skill + freshness + pricewatch + per-category) — the near-term value, US-safe.
3. **Phase B** sizing (scaffold during A; activate after GATE 1/2 + account connect).
4. **Phase C** exits/hedging.

## Decisions needed from you
- **Execution venue/eligibility** (GATE 2) — what compliant path, if any? Until resolved, B/C are
  logic-only.
- **CLV "close" definition** — T-4h vs T-1d snapshot + the liquidity floor.
- **Skill gates** — keep the $100k portfolio hard filter? Replace the 90%-win gate with a skill+CI
  threshold (recommended)?
- **Fractional-Kelly start** — quarter-Kelly (recommended) → half once leaders prove calibrated.

## Caveats carried from research
- CLV backfill on resolved markets is unreliable → must capture live (A4).
- Stake-implied edge depends on unobservable bankroll → noisiest input, shrink most.
- Polymarket maker-fee docs contradict each other → don't assume zero; verify live.
- v2 migration may affect watcher decode + trader signing → verify before trading.
- Bet-count thresholds are heuristics; the Lo/Buchdahl/Murphy formulas are the rigorous core.
