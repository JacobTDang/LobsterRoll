# Trader activation runbook (Phase B-4)

The full sizing path is already wired and code-complete: `strategy` sizes a signal
(`pkg/sizing` via `internal/sizer`), publishes `orders.proposed` with the stake →
notifier approval → `orders.approved` → `trader` calls `caps.Reserve(SizeUSD)` and
places (or rejects). The trader's hard caps are the backstop on top of sizing.

**Nothing below should be done until you are eligible to trade on Polymarket** (it
geofences US persons). This is a compliance decision, not a code step — the trader
ships `replicas: 0` and places no order until you scale it up on an eligible setup.

## Gates (both must clear before real orders)
1. **v2 signing.** Polymarket migrated to CTF Exchange V2 (2026-04-28); the official
   clients were re-released as `-v2`. Before scaling the trader, byte-check our
   hand-rolled EIP-712 against `py-clob-client-v2` as a golden oracle (sign the same
   order, compare the signature). See [[polymarket-v2-migration]],
   [[polymarket-clob-order-signing]]. The read path is already confirmed v2-OK
   (watcher decodes live v2 trades), but signing is only verifiable at a real placement.
2. **Eligibility.** Trade only via a path you are actually eligible for. Not engineered here.

## Step 1 — caps alignment (critical)
The trader caps must sit **above** normal sizing output, or they clip every sized
trade and defeat the engine. With `BANKROLL_USD = B`, `PerBetFrac = 0.03`,
`MaxExposureFrac = 0.10`:
- `MAX_USD_PER_TRADE` ≳ `0.03·B` (set comfortably above, as a safety ceiling)
- `MAX_OPEN_EXPOSURE_USD` ≈ `0.10·B`
- `MAX_USD_PER_DAY` per your daily risk tolerance

Example for `B = $10,000`: per-bet ≈ $300 → set `MAX_USD_PER_TRADE=500`,
`MAX_OPEN_EXPOSURE_USD=1000`, `MAX_USD_PER_DAY=2000`. If the caps are *below* sizing
(the current demo defaults are $25/$200/$500), every trade is clipped — fine for a
tiny test, wrong for real sizing.

## Step 2 — enable sizing in strategy
ConfigMap:
```
STRATEGY_SIZING_ENABLED: "true"
BANKROLL_USD: "10000"
KELLY_FRACTION: "0.25"   # start quarter-Kelly; raise to 0.5 once leaders prove calibrated
```
(strategy dials `LEADERBOARD_GRPC_ADDR` for track record + `STRATEGY_CLOB_BASE` for
the book — both already defaulted.) Strategy now sizes proposals; the trader is
still off, so this only changes the size shown in approval alerts. Verify the sizes
look sane in the dry run before going further.

## Step 3 — verify signing, then activate the trader
1. Fill the Secret: `TRADER_PRIVATE_KEY`, `POLYMARKET_API_KEY/SECRET/PASSPHRASE`;
   set `TRADER_MAKER_ADDRESS`/`TRADER_FUNDER_ADDRESS` in the ConfigMap.
2. Keep `EXECUTION_MODE: "approval"` — every order is human-approved (✅/❌) before it
   places. Do NOT start in `auto`.
3. Do the v2 signing byte-check (Gate 1) against a known order.
4. `kubectl -n lobsterroll scale deploy/trader --replicas=1`.
5. Place one small approved order; confirm fill + that `caps` decremented as expected.

## Follow-ups (not blocking)
- **Fill-vs-sellable reconciliation:** confirm "filled" vs "synced" vs sellable
  balance before re-using capital (pattern: `tosmart01/polymarket-position-watcher`).
- **`make verify-sizing`:** a local harness that runs a sample signal through the
  engine with stubbed leader stats + book and prints the stake + skip reasons — for
  tuning `KELLY_FRACTION`/caps without touching live trading.
- **Exposure feedback:** strategy currently sizes with `Exposure=0` (the trader
  enforces the real exposure cap). A strategy→trader exposure query would let sizing
  taper as exposure fills, rather than relying solely on the trader's hard cap.

## Why this is safe to ship now (with the trader off)
B-1/B-2 are pure/HTTP and fully tested. B-3 only changes the *proposed size* and is
off by default. The trader stays `replicas: 0`. So none of Phase B can place an
order until you complete Steps 1–3 on an eligible setup — the engine just makes the
*sizes* correct and ready.
