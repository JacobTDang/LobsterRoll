# Design: wallet tracking, full stats, and consensus signals

## Goal

Track the top **consistent-winner** wallets (not just big/lucky ones), keep their
full stats in our own DB, and detect when **multiple tracked wallets converge on
the same bet** — the strongest copy signal we have.

## Verified facts (live, 2026-06-17)

- Candidate pool: `lb-api.polymarket.com/profit?window={1d|7d|30d|all}&limit=N`
  and `/volume?...` → `[{proxyWallet, amount, name, pseudonym}]`. (Already used by
  leaderboard-svc — `/profit`=pnl, `/volume`=volume.)
- Per-wallet: `data-api.polymarket.com/value?user=` (portfolio $), `/positions?user=&sortBy=CURRENT`
  (open positions, `cashPnl`), `/activity?user=&limit=&offset=` (full trade history:
  type, side, usdcSize, conditionId, outcome, price), `/traded?user=` (count).
- **Win rate is computable from `/activity`**: group by `conditionId`; cash flow
  BUY −usdc / SELL +usdc / REDEEM +usdc / MERGE + / SPLIT −; a `REDEEM` marks the
  market resolved for that wallet; win = net cash > 0. Validated: Fredi9999 →
  65% over 29 resolved, $31M realized. No cheap win-rate endpoint exists — this
  crawl is the only way, so it runs as a periodic background job, cached in our DB.

## Architecture

```
 lb-api /profit,/volume ─┐
 data-api /activity,...  ─┴─► leaderboard-svc  (the "who to follow" brain)
                                 • candidate pool (multi-window)
                                 • per-wallet stats crawl  (win rate, realized PnL, ROI)
                                 • consistency ranking → top-N watchset
                                 • SQLite: watchset + wallet_stats
                                 • gRPC: StreamWatchset, GetWalletStats
                                      │ watchset
                                      ▼
 on-chain OrderFilled ─────► watcher-svc ──► trades.detected (NATS)
                                      ├──────────────► strategy-svc ─► orders.proposed
                                      └──────────────► consensus-svc (NEW)
                                                          • SQLite sliding window per tokenId
                                                          • ≥N distinct tracked wallets same side
                                                            within window W → consensus.signal
                                                                   │
 notifier-svc ◄── trades.detected (+ GetWalletStats) ── alert: trade + whale win-rate
              ◄── consensus.signal ──────────────────── 🔥 premium "N whales on X" alert
```

Two changes: **leaderboard-svc grows** stats+selection; **consensus-svc is new**.
(Consensus could be a strategy-svc module, but it's a distinct stateful concern
with its own DB, so a small dedicated service fits the existing per-concern split.)

## Data model (SQLite)

**leaderboard-svc**
- `watchset(proxy_wallet PK, rank, score, pseudonym, added_at)`
- `wallet_stats(proxy_wallet PK, win_rate, resolved_markets, realized_pnl,
   profit_7d, profit_30d, profit_all, portfolio_value, traded_markets, computed_at)`

**consensus-svc**
- `trade_events(token_id, condition_id, wallet, side, usdc, observed_at)` — appended
  per tracked trade; distinct wallets per token within window queried on each insert; old rows pruned.
- `fired(token_id, side, cohort_size, fired_at)` — dedup so we don't re-alert on every extra wallet.

## Stats + selection

1. Candidate pool = union of `/profit` top-K over **7d ∪ 30d ∪ all** (persistence
   across windows already filters one-week flukes).
2. For each candidate, crawl `/activity` (paginated, cached), compute win rate,
   resolved_markets, realized_pnl; pull `/value`, `/profit` per window.
3. Filter `resolved_markets ≥ MIN_RESOLVED` (e.g. 20) to exclude one-hit-wonders.
4. Score = `win_rate × log(1+realized_pnl)` (tunable); rank → **top-N watchset**.
5. Cadence: candidate crawl every ~12–24h (heavy); watchset stats refresh ~6h;
   watchset streamed to watcher continuously (existing path).

## Consensus engine (the signal)

- Key = `tokenId` (uniquely identifies market+outcome) + `side`.
- On each `trades.detected` from a tracked wallet: insert into `trade_events`,
  prune older than window `W` (e.g. 6h), count **distinct wallets** on the same
  token+side in `W`.
- When count ≥ `CONSENSUS_MIN` (e.g. 3) and not already fired at this cohort size:
  emit `consensus.signal{tokenId, conditionId, side, wallets[], combinedUSD, count, window}`.
- Re-fire only when the cohort grows (e.g. +1 wallet), with a cooldown.
- Restart-safe: the window lives in SQLite, so a restart rebuilds counts from rows.

## Alerts

- Per-trade alert (existing) gains a whale-stats line from `GetWalletStats`:
  `👤 65% win (29 mkts) · realized +$31M · $1.2k portfolio` (cached; no per-alert data-api hit).
- `consensus.signal` → premium alert:
  `🔥 CONSENSUS — 4 tracked wallets BUY "Will X?" → YES in 3h (combined $12k, avg 61% win)`.
- Optional: strategy-svc boosts size / auto-approves when consensus backs a proposal.

## Phasing

- **P1 Foundation** — data-api client; verify win-rate algo (done); `wallet_stats`
  schema; crawl+compute+store; consistency selection; `GetWalletStats` gRPC.
- **P2 Alerts** — notifier shows whale stats from leaderboard cache (supersedes the
  earlier per-alert data-api idea — sourced from our DB instead).
- **P3 Consensus** — consensus-svc (sliding window) → `consensus.signal` → premium alert.

## Tunables / open questions

- `MIN_RESOLVED`, score formula, `CONSENSUS_MIN`, window `W`, re-fire policy.
- Win-rate v1 counts only **redeemed** markets; wallets that exit before resolution
  aren't counted (refine later by also treating net-zero positions as closed).
- Crawl depth per wallet (activity pagination cap) vs cost.
