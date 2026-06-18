# Design: "track my positions" priority exit alerts (planned)

Goal: when a tracked whale **exits (SELL) a market the user currently holds**, send
a distinct PRIORITY Telegram alert (a cue to consider exiting too). Read-only, needs
only the user's PUBLIC wallet address — no key, no trade risk. Off unless `USER_WALLET` set.

## Verified live
`GET https://data-api.polymarket.com/positions?user=<wallet>` returns rich rows incl.
`asset` (held outcome tokenId), `conditionId`, **`oppositeAsset`** (opposite outcome
tokenId), `outcome`, `size`, `avgPrice`, `curPrice`, `currentValue`, `title`, `slug`,
`redeemable`. The `oppositeAsset` field enables same-market/opposite-outcome matching
WITHOUT an enrichment round-trip on the hot path.

## Where: extend notifier-svc (not a new service)
`handler.Handle(TradeDetected)` already has wallet/token/side/size + enrichment +
stats + the Telegram sender + the dedup set. Add an in-memory position cache + a
matcher + a branch in Handle. (Consensus is its own service only because it needs
windowed cross-trade state; position matching is a stateless per-trade lookup.)

## Pieces
- `internal/positions/client.go` — `Positions(ctx, wallet)` via `pkg/httpx.Get` (mirror dataapi).
- `internal/positions/positions.go` — `Cache` with atomic Snapshot swap; indexes
  `byToken[asset]` and `byOpposite[oppositeAsset]`; filters dust (size≈0) and
  resolved (`redeemable && curPrice==0`). Poller refreshes every `MY_POSITIONS_POLL_INTERVAL`
  (default 5m), keeps the last snapshot on fetch error, suppresses matching if stale (>6×interval).
- `internal/mypos/match.go` — pure matcher. Direction matrix:
  | whale action | meaning | fire? | priority |
  |---|---|---|---|
  | SELL same token | exiting YOUR outcome | yes | HIGH (the named feature) |
  | BUY opposite token | betting against you | yes | MEDIUM |
  | BUY same token | doubling down | opt-in (`MY_POSITIONS_ALERT_ON_ADD`) | LOW |
  | SELL opposite token | ambiguous | no (phase 1) | — |
  Ignores the user's own wallet (self-trade guard).
- `format.FormatMyPositionAlert` — distinct banner ("⚠️ WHALE EXITING A MARKET YOU HOLD"),
  title/slug from the cached holding (renders even if enrichment is down).
- Sent as a SEPARATE priority message (in addition to the normal alert), deduped via
  the existing TTL set with a `mypos:` key prefix; un-cached on send failure.

## Config (notifier)
`USER_WALLET` ("" = off), `MY_POSITIONS_POLL_INTERVAL` (5m), `MY_POSITIONS_ALERT_ON_ADD`
(false), `DATA_API_BASE`. `USER_WALLET` is public → ConfigMap (commented off by default).

## Phasing
1. SELL-same-token exit alerts, end-to-end, behind `USER_WALLET`.
2. opposite-BUY ("against you") + same-BUY ("doubling down") behind a flag; unrealized PnL.
3. multi-outcome/conditionId matching; per-holding prefs.

## Tests
match (table-driven: each direction, self-guard, nil/stale snapshot), cache
(index/dust/resolved filters, -race, staleness), client (httptest off the real JSON
fixture incl. oppositeAsset), format (banner variants), handler (both messages on a
held-token SELL; deduped; cache nil = off), config. Dry-run verify: set USER_WALLET,
inject a trade on one of its held tokens, assert the priority message.

(Plan produced by an ultrathink planning pass; see [[feature-user-position-exit-alerts]].)
