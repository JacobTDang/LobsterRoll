# Design Plan: Phase B — position sizing engine

Status: **plan only.** Turns a copy signal into a concrete, risk-bounded stake. The sizing/risk
*logic* is jurisdiction-neutral and buildable+testable now; the *execution* it feeds is gated (see
below). Formulas are in `docs/PLAN-roi-skill-sizing.md` (Problem 2); this doc maps them onto our
services. Inputs now exist: the leaderboard serves per-wallet shrunk ROI, skill, CLV, portfolio.

## Two gates (execution only — not the math)
- **GATE 1 — v2 signing.** The trader's EIP-712 must be verified against CTF Exchange V2 before any
  order. Read path already confirmed v2-OK by live observation; signing is verifiable only when an
  order is actually placed → do it at activation. See [[polymarket-v2-migration]].
- **GATE 2 — US eligibility.** Polymarket geofences US persons from trading. Phase B builds the
  sizing/risk engine and the (inert) execution wiring; **actually enabling trades requires an
  eligible path** — a decision, not something this plan engineers around the geoblock. The trader
  ships `replicas: 0`; nothing places an order until you scale it up on an eligible setup.

So B-1/B-2 (pure logic) ship and are unit-tested now; B-3/B-4 wire them but stay inert behind the
gates.

## Architecture: sizing lives in `strategy`; `trader` enforces hard caps
`strategy` already consumes `trades.detected`, applies the execution policy, and emits
`orders.proposed`. Phase B replaces its naive size with the engine: per signal it gathers inputs,
computes a stake, and proposes it (or skips). `trader` keeps the hard risk caps it already has
(`caps` pkg: per-trade/day/exposure, atomic check-and-commit, halt switch) as defense-in-depth — a
bug in sizing can never exceed the trader's ceiling.

### Inputs (all already available)
- **Leader edge** — `strategy` calls leaderboard `GetWalletStats(leader)` → shrunk ROI, `SkillScore`,
  `AvgCLV`/`CLVN`, `Fresh`, `PortfolioValue`. (gRPC client; leaderboard is already a server.)
- **Leader conviction** — the trade's USD (`Size*Price`) relative to their `PortfolioValue`.
- **Market price + spread + depth** — new CLOB `/book` client (`/book`,`/spread`,`/midpoint`),
  microprice, and an order-book walk for slippage. (HTTP → httptest-testable.)
- **Our bankroll + open exposure** — config bankroll + tracked exposure state (in `trader`, which
  already tracks daily spend/exposure in `caps`).

## The sizing pipeline (maps Problem-2 formulas to our data)
For a signal on token at market mid `p` (devigged):
1. **Edge proxy `q`** (shrink HARD toward the market price — it's a strong prior on liquid markets):
   - CLV path (preferred, lowest-variance): `q_clv = p + AvgCLV`, confidence `w_clv = min(1, CLVN/clvFull)`.
   - Track-record path: invert ROI/skill to an edge; lower weight.
   - Conviction path (noisiest — shrink most): from `tradeUSD / PortfolioValue` percentile.
   - Blend → Bayesian-shrink toward `p` with high prior strength (≈ market-implied). Net `q` stays
     close to `p`; only strong, well-sampled CLV moves it materially.
2. **Kelly** `f* = (q − p)/(1 − p)`; bet only if `f* > 0`. Use **fractional k = 0.25** (start), → 0.5
   once leaders prove calibrated.
3. **Cost haircut** `edge_eff = (q − p) − half_spread − fee − slippage`. **Skip** if `edge_eff ≤ buffer`
   (≈2–3%) or `spread > maxSpread`. Polymarket taker fee = `shares·feeRate·p·(1−p)`.
4. **Size** `stake = min(k·f*·bankroll, perBetCap·bankroll)`, then cap by **depth** (≤~20% of book,
   ≤~5% of volume — walk the book so VWAP slippage stays in tolerance).
5. **Portfolio caps** total exposure ≤ 6–10% (a correlated same-event cluster counts as one bet);
   **drawdown breaker** — halve size at 5–10% DD, stop at 15–20% (off the trader's tracked equity).
6. **Prefer consensus** — research shows single-leader copying underperforms; size consensus signals
   (our differentiator) higher than single-wallet ones, and require `Fresh` (don't size cooling
   wallets — already gated out of the watchset, but enforce here too).

## Phasing (each independently verifiable)
- **B-1 — `pkg/sizing` (pure, now):** Kelly + fractional, edge blend + shrink-to-prior, cost haircut
  + skip rules, bankroll/per-bet/exposure/drawdown caps. All pure functions → table-driven unit tests
  + mutation. Jurisdiction-neutral; ships immediately.
- **B-2 — CLOB book client (now):** `/book`,`/spread`,`/midpoint` via `pkg/httpx`; microprice +
  slippage-walk helper. httptest-tested. (Reuses the price-fetch patterns from pricewatch.)
- **B-3 — strategy integration (gated):** dial leaderboard (stats) + the book client; per signal run
  the pipeline → emit a sized `orders.proposed` (or skip with a logged reason). Bankroll config +
  exposure/drawdown state. Inert in approval mode (still just proposes; user approves).
- **B-4 — trader activation (gated, last):** verify v2 signing (GATE 1) against `py-clob-client-v2`
  as a golden oracle; confirm `caps` ceilings sit above the sizing output; document the
  `replicas 0→1` activation runbook (GATE 2). Fill-vs-sellable reconciliation
  (pattern: `tosmart01/polymarket-position-watcher`).

## Config (new)
`BANKROLL_USD`, `KELLY_FRACTION` (0.25), `MAX_SPREAD`, `EDGE_BUFFER`, `MAX_DEPTH_FRAC`,
`MAX_EXPOSURE_FRAC`, `DD_DERISK`/`DD_STOP`, `CLV_FULL` (edge confidence). Trader caps stay as the
hard ceiling.

## Honest caveats
- **Copy-trading underperforms on average** (Apesteguia et al.) and Polymarket P&L is concentrated —
  hence: fractional Kelly, hard shrink to the market prior, consensus-preferred, fresh-required.
- **Latency/adverse selection:** by the time we copy, the price has moved; subtract slippage *before*
  sizing and skip when the edge is gone. More copiers worsen this — it's the mechanism by which a
  leader's edge decays once crowded.
- **CLV cold-start:** the edge proxy leans on CLV, which is sparse early (forward-accumulating); until
  `CLVN` builds, sizing leans on ROI/skill + heavier shrink → smaller, more conservative stakes
  (correct).
- **Maker-fee docs conflict** — verify the live fee at activation; don't assume zero.
- **No real-money order until BOTH gates clear.** B-1/B-2 are safe to build/test now; B-3/B-4 stay
  inert (approval-only / replicas 0) until you're eligible and v2 signing is verified.

## Verification
B-1/B-2 are pure/HTTP → fully unit + mutation tested in-sandbox (no live network, US-safe). B-3 is
exercised with fakes (stub leaderboard + book) asserting the proposed size for a given signal. B-4's
live placement is verified only at activation against a real (eligible) account — document a
`make verify-sizing` that runs a signal through the engine with stubbed inputs and prints the stake +
skip reasons.
