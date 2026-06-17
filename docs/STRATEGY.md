# LobsterRoll — Strategy Research & Feasibility (2026)

Synthesis of a 19-agent research sweep into durable, **legitimate** edges for a *solo
operator* (Go microservices, modest capital scaling later). Companion to
[PLAN.md](../PLAN.md) and [IMPLEMENTATION.md](IMPLEMENTATION.md). Every figure is sourced
in the per-strategy sections; treat ROI ranges as evidence-based estimates, not promises.

## The hard truth (read first)
- **This is a hostile market.** ~**84% of traders lose money**; <1% of wallets take ~half the
  profits; **14 of the top 20 wallets are bots**; Jump/SIG/DRW/Wintermute now run desks here.
- **You cannot win on speed.** Arb windows compressed to **~2.7s**, 73% of arb goes to
  sub-100ms bots colocated in London/Dublin (~5ms) vs a US home box (~70–130ms). Polymarket
  also added **dynamic fees specifically to kill latency arb.** So: **no latency races.**
- **Maker-only is the throughline.** Nearly every viable edge requires posting **passive limit
  orders** (0 fees + 20–25% rebate), not taking (fees up to ~1.8%/leg + slippage). Taking
  destroys most paper edges.
- **The durable edges for you are slow, non-adversarial, analysis- or yield-based:**
  market-making, mean-reversion, structural/logical arb in fee-free niches, calibration bias.
- ⚠️ **Reframe of LobsterRoll's premise:** *blind copy-trading underperforms its source wallets
  by 60–80%* and is legally fraught — so wallet-tracking becomes a **weighted signal input**,
  not the core strategy.

## Cross-cutting facts that change the build
- **CLOB v2 / CTF-Exchange v2 migration (2026-04-28):** old subgraph + v1 pipelines are dead;
  new **`pUSD` collateral** (ERC-20, 1:1 USDC). Must target **`clob-client-v2`** equivalents.
  ➜ `IMPLEMENTATION.md` contract/USDC assumptions need a **verify pass** before Phase 2/7.
- **Fee formula:** `fee = shares × categoryRate × p × (1−p)` — peaks at $0.50, ~0 at extremes.
  Rates: Crypto ~1.8%, Econ ~1.5%, Politics/Finance/Tech ~1.0%, Sports ~0.75%, **Geopolitics 0%**.
  **Makers pay 0 and earn 20–25% of taker fees** as daily pUSD rebates.
- **US legality:** trade only on **Polymarket US (CFTC-regulated DCM, QCEX)** with full **KYC**;
  **no VPN** (instant freeze); avoid **Minnesota/Nevada**; **bots are allowed** but DOJ+CFTC are
  filing **criminal insider cases**, and an **April-2026 ban wave hit rapid place/cancel "ghost"
  orders** — so quote-refresh must be paced + audit-logged. Single wallet only.
- **Infra:** Pi is fine for slow strategies; anything time-sensitive belongs on a **~$20–40/mo
  Dublin/London VPS** (keep Pi as control plane). Go libs already exist:
  `ivanzzeth/polymarket-go-contracts`, `polymarket-go-gamma-client` (incl.
  `find-negrisk-opportunities`), `warproxxx/poly_data`.
- **Backtest data:** trades+resolutions **free & deep** (HuggingFace "Polymarket-v1" ≈1.2B fills
  to 2022; Dune; on-chain). **L2 order-book depth only from ~Oct 2025** (Telonex ~$79/mo).
  ⚠️ **Filter relayer trades** (inflate volume ~53%).

## Feasibility matrix

| # | Strategy | Verdict | Net edge (solo) | Min capital | Pi-ok? |
|---|----------|---------|-----------------|-------------|--------|
| 1 | **Mean-reversion / overreaction-fade** | **Viable** | ~12–35%/yr | $5–20k | ✅ |
| 2 | **Market-making + liquidity rewards** | **Viable (yield)** | ~10–25%/yr net* | $5–10k | ✅ (kill-switch) |
| 3 | **Cross-market / logical arb** | Marginal–Viable | ~25–100%/yr | $5–20k | ✅ |
| 4 | **Favorite-longshot / calibration bias** | Marginal–Viable | ~15–40%/yr | $5–15k | ✅ |
| 5 | **NegRisk convert arb (niche, fee-free)** | Marginal | $50–400/mo | $2–5k | ✅ |
| 6 | **Fair-value model (esports/low-tier soccer)** | Conditional | ~1–3%/mo | $5–10k | ✅ (+odds API) |
| 7 | **Smart-money wallet-intel (weighted signal)** | Marginal | 15–35%/yr | $2–5k | ✅ |
| 8 | **Sports speed-to-truth (long-tail games)** | Marginal | situational | $5–25k | VPS better |
| 9 | **News-reactor LLM (slow-lane politics)** | Conditional | 10–25%/mo (hi-var) | $5–10k | VPS + APIs |
| 10 | **Cross-platform Kalshi arb** | Marginal | 5–15%/yr | $10k (split) | VPS better |
| 11 | **UMA near-resolution yield** | Viable (narrow) | ~5–20%/yr | $10–25k | ✅ |
| — | **Crypto latency arb** | **❌ Not viable** | negative | — | ❌ |
| — | **Blind copy-trading** | **❌ Avoid** | −60–80% vs source | — | — |

*MM: rewards/rebates are real (combined ~15–40% gross) but **adverse selection nets it down to
~3–12%** unless market selection + a news velocity kill-switch are disciplined; ~10–25% is the
realistic disciplined band.

## Recommended roadmap for a solo Pi operator
**Phase A — Foundations (mandatory, gate everything):**
`backtester` (HuggingFace + Telonex L2, relayer-filtered) · `risk-manager` (quarter-Kelly,
3%/40%/15% caps, drawdown brakes) · `execution-svc` (**maker-default**, GTC/GTD/post-only,
depth-aware slicing, heartbeat/dead-man switch).

**Phase B — First live edges (non-adversarial, Pi-friendly):**
1. **Mean-reversion** (20-period Z-score, fade >2σ, **limit orders only**, VPIN + resolution guards).
2. **Market-making** on mid-liquidity **long-dated / geopolitics (0-fee)** markets w/ kill-switch.

**Phase C — Structural & systematic adds:**
3. **Logical-arb scanner** (Class B same-event sums first — free from Gamma groupings — then date-monotonicity, nesting, semantic dupes).
4. **NegRisk scanner** (fork `find-negrisk-opportunities`; fee-free niches; `sum_ask<0.96`).
5. **Calibration/favorite-longshot** signal (maker at extremes).

**Phase D — Signals & specialization (optional/later):**
6. **wallet-intel** as a *weighted prior* (not blind copy; insider-pattern = log-don't-trade).
7. **fair-value** (esports/low-tier soccer via Pinnacle/Betfair anchor).
8. News-reactor / sports speed-to-truth / Kalshi arb — only if moved to a VPS.

All emit `orders.proposed` → `risk-manager` → `trader`; the existing bus/gRPC scaffold already supports this.

---

## Per-strategy briefs (condensed)

### 1. Mean-reversion / overreaction-fade — **Viable (top pick)**
Strongest *Polymarket-specific* academic backing: **58% negative daily serial correlation** in
presidential markets; a parameterized backtest showed **+22–31% CAR, Sharpe 1.2–1.96 at 10bps**
— **but only with passive limit orders** (market orders zero out the alpha). Build: 20-period
Z-score on 10-min bars, enter on >2σ deviation as maker, exit on mean-revert / 5-period stop;
**filter VPIN>0.75** (don't fade informed flow) and **min $10k 24h volume**; hard **no-hold
within 48h of resolution.** Diversify 20+ markets.
Sources: QuantPedia mean-reversion study; arxiv 2606.04217 (VPIN); DL News (Vanderbilt).

### 2. Market-making + liquidity rewards — **Viable yield, with discipline**
$5M+/mo rewards pool; **public quadratic score `((maxSpread−yourSpread)/maxSpread)² × size`**,
two-sided required, 1-min sampling, midnight-UTC epochs; stacks with **maker rebates (20–25%)**
+ **4% holding APY**. Gross 15–40%/yr but **adverse selection is the killer** → net ~3–12% naive,
~10–25% disciplined. Build: reward-market scanner → mid-tracker (WS) → two-sided quote engine
(Avellaneda-Stoikov skew) → **news velocity kill-switch** (cancel on >2¢ tick move; no quoting
<6–24h to resolution) → rewards ledger. Target long-dated/geopolitics; **deploy quoting to a
Dublin VPS** for safe cancels. Note: pace cancels to avoid the ghost-order ban.
Sources: docs.polymarket.com/market-makers/*; arxiv 2604.24366 (microstructure); warproxxx/poly-maker.

### 3. Cross-market / logical arb — **Marginal→Viable**
Bigger historical bucket (~$29M multi-condition vs $10.6M simple); median window **~16s** (vs
3.6s simple). Build order: **Class B** mutually-exclusive sums (free from Gamma `event` groupings)
→ **C** date-series monotonicity (regex) → **A** nesting (entity extraction) → **D** semantic
dupes (embeddings + LLM verify). Trade as maker, geopolitics (0-fee) first, edge>200bps.
Sources: IMDEA "Probabilistic Forest"; arxiv 2605.00864, 2601.01706.

### 4. Favorite-longshot / calibration bias — **Marginal→Viable, durable**
On Polymarket the bias **reverses sportsbook lore**: cheap longshots (0–0.10) **overpriced**
(−0.23%/contract), favorites (0.80–0.90) **underpriced** (+0.98%); Kalshi study: sub-10¢ buyers
lose >60%, **makers** above 50¢ earn ~+1.9–2.6%. Harvest as **maker** (buy underpriced favorites
/ NO-side of overpriced longshots); fees ~0 at extremes. Slow (days–weeks), capital can idle.
Sources: arxiv 2606.04217; UCD WP2025_19 / CEPR.

### 5. NegRisk convert arb — **Marginal (niche supplement)**
`convertPositions()` is **fee-free**; arb when Σ(YES asks) < $1. Contracts: NegRiskAdapter
`0xd91e80cf2e7be2e162c6513ced06f1dd0da35296`, NegRisk CTF Exchange `0xc5d563a36ae78145c45a50134d48a1215220f80a`,
Fee Module `0x78769d50be1763ed1ca0d5e878d93f05aabff29e`. Liquid events owned by bots; **target
newly-launched niche geopolitics sets**, `sum_ask<0.96`. **Go libs exist** (fork
`find-negrisk-opportunities`). Realistic $50–400/mo. Watch sequential-leg execution risk.
Sources: neg-risk-ctf-adapter; pkg.go.dev ivanzzeth; IMDEA paper.

### 6. Fair-value modeling — **Conditional (esports/low-tier soccer)**
Edge = de-vigged **Pinnacle/Betfair** lines as anchor to fade Polymarket in **esports + lower-tier
soccer** (a real bot did **5.2% net/3mo**). **Skip elections** (whale manipulation — "French
Whale" moved Trump 10–15pts on $85M), **skip in-play, skip major outrights** (efficient).
Pinnacle public API closed → paid vendor ~$50–150/mo.
Sources: kacho.io real-numbers; sports-ai.dev; sportsapis.dev.

### 7. Smart-money wallet-intel — **Marginal (as weighted signal)**
~7.6% of wallets profitable; profitable-wallet traits (200+ resolved, >55% WR, diversified,
recent). **Don't blind-copy** (−60–80% vs source, crowded, slippage). Build credibility-scored,
alpha-decayed signal (Brier-weighted) feeding a *prior*; **insider-pattern wallets → log, don't
trade** (legal red line: DOJ/CFTC charged Van Dyke $409k, Spagnuolo $1.2M in 2026). Public data
only; document methodology.
Sources: CoinDesk (<1% take half); data-api; Sidley/Debevoise legal.

### 8. Sports speed-to-truth — **Marginal (long-tail only)**
Markets stay open a **median ~22–34 min after the event ends** (LFMP locks settlement, so trading
the lagging book is legitimate). Marquee games owned by colocated bots; **edge is obscure games**
(mid-season NBA, CL group stage) where stale orders sit minutes. Feeds: ESPN unofficial (free,
5–15s) / API-Sports (~$10–50/mo). **VPS beats Pi** here.
Sources: PolySyncer resolution study; docs.polymarket.us sports FAQ.

### 9. News-reactor LLM — **Conditional (slow-lane politics)**
Window 30s–5min on politics/world events; **avoid crypto** (LLMs badly miscalibrated; benchmarks
show most LLMs **lose** money live). Pipeline: news (RSS/NewsData $200/mo) → cheap-LLM filter →
capable-LLM probability → maker order. ~$300–400/mo cost; high variance. VPS recommended.
Sources: PolyBench / Prediction Arena (arxiv); QuantVPS latency.

### 10. Cross-platform Kalshi arb — **Marginal (learning module)**
Real but windows ~2.7s, institutions present; capital must sit on **both** venues; needs
**Polymarket US** + Kalshi (flat $0.02/contract, RSA-PSS auth). 5–15%/yr at <$50k. Value is the
reusable dual-API/market-matching infra. **Resolution-rule mismatch** can turn "arb" into a 2-way
loss — vet criteria.
Sources: laikalabs; docs.kalshi.com; CoinDesk hiring-wave.

### 11. UMA near-resolution yield — **Viable but narrow**
Buy near-certain winners trading **$0.93–$0.99** before settlement, hold to redemption (median
**41 min**, P90 ~6h, disputed +49h). **Legit only — no oracle manipulation.** ⚠️ Two things
shrank it: the **MOOV2 upgrade (Aug 2025)** put 37 whitelisted proposers at 99.7% accuracy,
compressing the harvest window toward minutes on liquid markets; and **dispute asymmetry is
brutal** — at $0.95 entry, one wrong call wipes 19 wins, and the highest-yield markets are the
ambiguous-criteria ones most likely to be disputed (1% overall, **4.8% geopolitics**). Build an
`oracle-monitor`: subscribe to Polygon logs on the **UmaCtfAdapter** contracts (v3.0
`0x157Ce2d672854c848c9b79C49a8Cc6cc89176a49`; NegRisk `0x2F5e3684cb1F318ec51b00Edba38d79Ac2c0aA9d`)
+ OptimisticOracle V2/V3, run a resolution state machine, and **require independent outcome
confirmation** before buying. Hard filters: skip subjective wording, skip >$5M-OI ambiguous
markets, require >$50k volume. This is also the **risk gate** for the sports speed-to-truth play.
*Realistic ~5–20%/yr (windows narrowing post-MOOV2); per-trade annualized math looks huge but is
capacity-constrained.*
Sources: PolySyncer resolution study; docs.polymarket.com/developers/resolution/UMA; startpolymarket bonding; uma.xyz managed-proposers.

---

## What to skip (evidence-backed)
- **Crypto 15-min latency arb** — fees + colocation killed it for retail. Negative EV on a Pi.
- **Blind copy-trading** — underperforms source 60–80%; a popular copy-bot repo shipped
  key-stealing malware (Dec 2025). Use scored *signals* instead.
- **MM on liquid politics / major crypto** — Jump/SIG/Flow own these; tight spreads, high toxicity.

## Realistic expectations
Starting bankroll to matter: **$5k experiment / $25–50k for income-level.** Blended target with a
disciplined multi-strategy book: **~2–4%/month with tight drawdown** (one documented diversified
book did 11.7% over ~2 quarters, 3.2% max DD). **Time to real profitability: 12–24 months**, first
6 as R&D/paper-trading. Backtests that fill at mid overstate live returns **30–100%** — model
slippage or you will fool yourself.
