# Phase C — Exit / Hedge Manager (gated)

`pkg/exit` is the pure decision engine for managing an already-open (copied)
position. It mirrors `pkg/sizing`: deterministic, no I/O, fully unit-tested, and
**not wired to execution** — activation is gated behind the same trader
eligibility checks (US geoblock + v2 signing) as Phase B.

> Note: because nothing imports `pkg/exit` yet, the whole-program `deadcode` tool
> will report its exported functions as unreachable. That is expected — it is a
> staged, gated capability, not dead code. It activates when the exit manager is
> wired into the trader (see "Activation" below).

## Decision

`exit.Decide(Position, Config) Action` returns one recommended action by priority:

1. **Stop loss** — `CurPrice <= StopLoss` → sell ALL. Risk first; beats every other rule.
2. **Leader exited** — the copied whale left this market → sell `LeaderExitFrac` of the position.
3. **Hedge-lock** — `1 - EntryPrice - OppPrice >= HedgeLockMin` → buy an equal share
   count on the opposite outcome for a guaranteed payout (free profit when the two
   sides sum to < 1). `LockableProfitFrac(entry, opp)` exposes the per-share lock.
4. **Take profit** — `CurPrice >= TakeProfit` → scale out `TakeProfitFrac` of the position.
5. **Hold** — otherwise.

A zero threshold disables its rule. An unset/invalid fraction defaults to a FULL
exit (`clampFrac`) so a misconfig never silently no-ops an intended exit.

The **leader-exited** input is the bridge to the existing position tracking: the
notifier already detects when a tracked whale exits a market the user holds
(`services/notifier/internal/positions`). That same signal feeds `LeaderExited`.

## Activation (deferred, gated)

When trading is eligible:
1. A position store (entry price + shares per held market) feeds `Position`.
2. A loop polls live mids (CLOB `/book`, already in `services/strategy/internal/book`)
   + the leader-exit signal, calls `Decide`, and emits an `OrderProposal`
   (Sell or Hedge) onto the bus — reusing the trader's approval/caps/halt net.
3. Sizing caps (`pkg/sizing`) still bound any resulting buy (the hedge leg).

Until then the engine ships as tested library code only.
