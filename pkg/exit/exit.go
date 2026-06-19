// Package exit is the pure decision engine for managing an already-open (copied)
// position: when to take profit, cut a loss, hedge-to-lock, or follow the copied
// leader out. It computes a single recommended Action from prices + config and
// performs NO I/O — execution is gated behind the trader's eligibility checks
// (US geoblock + v2 signing), exactly like the sizing engine.
package exit

// Kind is the type of action the engine recommends.
type Kind int

const (
	Hold  Kind = iota // do nothing
	Sell              // sell Shares of the held outcome (reduce/close)
	Hedge             // buy Shares of the OPPOSITE outcome to lock a guaranteed payout
)

func (k Kind) String() string {
	switch k {
	case Sell:
		return "sell"
	case Hedge:
		return "hedge"
	default:
		return "hold"
	}
}

// Action is the engine's recommendation. Shares is the quantity to sell (Sell) or
// the opposite-outcome shares to buy (Hedge); zero for Hold.
type Action struct {
	Kind   Kind
	Shares float64
	Reason string
}

// Position describes the held position and live prices (all prices in [0,1]).
type Position struct {
	EntryPrice float64 // average entry price of the held outcome
	CurPrice   float64 // current mid price of the held outcome
	OppPrice   float64 // current mid price of the opposite outcome (for hedging)
	Shares     float64 // shares currently held
}

// Config tunes the engine. A zero threshold disables that rule.
type Config struct {
	StopLoss       float64 // sell ALL if CurPrice <= StopLoss
	TakeProfit     float64 // scale out when CurPrice >= TakeProfit
	TakeProfitFrac float64 // fraction of current shares to sell at take-profit; (0,1], and an unset/invalid value sells the FULL position (clampFrac)
	HedgeLockMin   float64 // recommend a hedge when lockable profit fraction (1-entry-opp) >= this
	LeaderExited   bool    // the copied leader has exited this market
	LeaderExitFrac float64 // fraction of current shares to sell when the leader exits (0,1]
}

// LockableProfitFrac is the per-share profit locked by a full equalizing hedge:
// hold 1 YES share (cost entry), buy 1 NO share (cost opp) -> guaranteed payout 1
// in either outcome, for total cost entry+opp. Positive only when the two sides
// sum to less than 1.
func LockableProfitFrac(entry, opp float64) float64 { return 1 - entry - opp }

// Decide returns the single highest-priority recommended action. Priority:
// stop-loss (risk) > leader exit (the smart money left) > hedge-lock (lock
// guaranteed profit once the lockable-profit threshold is met) > take-profit >
// hold. Priority is strictly by rule order; it does not compare expected values
// between rules.
func Decide(p Position, cfg Config) Action {
	if p.Shares <= 0 {
		return Action{Kind: Hold, Reason: "no position"}
	}

	if cfg.StopLoss > 0 && p.CurPrice <= cfg.StopLoss {
		return Action{Kind: Sell, Shares: p.Shares, Reason: "stop loss"}
	}

	if cfg.LeaderExited {
		frac := clampFrac(cfg.LeaderExitFrac)
		return Action{Kind: Sell, Shares: p.Shares * frac, Reason: "leader exited"}
	}

	if cfg.HedgeLockMin > 0 && LockableProfitFrac(p.EntryPrice, p.OppPrice) >= cfg.HedgeLockMin {
		// Full equalizing hedge: buy the same share count on the opposite side.
		return Action{Kind: Hedge, Shares: p.Shares, Reason: "lock profit (hedge)"}
	}

	if cfg.TakeProfit > 0 && p.CurPrice >= cfg.TakeProfit {
		frac := clampFrac(cfg.TakeProfitFrac)
		return Action{Kind: Sell, Shares: p.Shares * frac, Reason: "take profit"}
	}

	return Action{Kind: Hold, Reason: "hold"}
}

// clampFrac bounds a fraction to (0,1], defaulting an unset/invalid value to a
// full 1.0 so a misconfigured fraction never silently no-ops an intended exit.
func clampFrac(f float64) float64 {
	if f <= 0 || f > 1 {
		return 1
	}
	return f
}
