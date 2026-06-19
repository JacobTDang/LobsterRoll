// Package sizing turns a copy signal into a risk-bounded stake. It is pure and
// jurisdiction-neutral (no I/O, no execution) — the strategy service feeds it
// live inputs and the trader enforces hard caps on top. See
// docs/PLAN-phase-b-sizing.md and docs/PLAN-roi-skill-sizing.md (Problem 2).
//
// Pipeline: infer an edge over the market price (CLV-led, ROI fallback, shrunk by
// fractional Kelly), subtract trading costs, size by fractional Kelly, then clamp
// to per-bet / depth / total-exposure caps and a drawdown brake.
package sizing

// Inputs are the live, per-signal sizing inputs.
type Inputs struct {
	Price        float64 // devigged market mid for the outcome, 0..1
	HalfSpread   float64 // (ask-bid)/2 in price units
	FeeRate      float64 // Polymarket taker fee rate (0..~0.07)
	Slippage     float64 // estimated price impact for the intended size (price units)
	AvgCLV       float64 // leader's mean closing-line value (price units)
	CLVN         int     // leader's settled-CLV sample count
	ShrunkROI    float64 // leader's sample-shrunk ROI (skill-adjusted)
	Fresh        bool    // leader not cooling off
	Bankroll     float64 // our bankroll (USD)
	Exposure     float64 // our current open exposure (USD)
	DrawdownFrac float64 // current drawdown from high-water, 0..1
	DepthCapUSD  float64 // max stake the book can absorb within slippage tolerance
}

// Config is the (tunable) sizing policy.
type Config struct {
	KellyFraction   float64 // fraction of full Kelly (e.g. 0.25)
	EdgeBuffer      float64 // require effective edge above this (e.g. 0.02)
	MaxSpread       float64 // skip if full spread exceeds this (e.g. 0.02)
	PerBetFrac      float64 // per-bet cap as a fraction of bankroll
	MaxExposureFrac float64 // total open-exposure cap as a fraction of bankroll
	DDDerisk        float64 // halve sizing at/above this drawdown
	DDStop          float64 // stop entirely at/above this drawdown
	CLVFull         int     // settled-CLV samples for full CLV confidence
	MinStakeUSD     float64 // skip stakes below this
}

// Decision is the sizing result: Stake>0 to bet, or Stake==0 with a Reason.
type Decision struct {
	Stake  float64
	Reason string // why it was skipped (empty when sized)
}

func skip(reason string) Decision { return Decision{Reason: reason} }

// Size computes the stake for a signal, or a skip with a reason.
func Size(in Inputs, cfg Config) Decision {
	switch {
	case in.Bankroll <= 0:
		return skip("no bankroll")
	case in.Price <= 0 || in.Price >= 1:
		return skip("price out of range")
	case !in.Fresh:
		return skip("leader cooling off")
	case 2*in.HalfSpread > cfg.MaxSpread:
		return skip("spread too wide")
	case in.DrawdownFrac >= cfg.DDStop:
		return skip("drawdown stop")
	}

	// Edge over the market price (price units): CLV-led, ROI fallback. CLV weight
	// ramps with sample count; with no CLV it leans entirely on the (already
	// shrunk, conservative) ROI signal. Negative edge => no bet.
	wCLV := float64(in.CLVN) / float64(cfg.CLVFull)
	if wCLV > 1 {
		wCLV = 1
	}
	roi := in.ShrunkROI
	if roi < 0 {
		roi = 0
	}
	edgeROI := in.Price * roi // q_roi - p = p*(1+ROI) - p
	edge := wCLV*in.AvgCLV + (1-wCLV)*edgeROI
	if edge <= 0 {
		return skip("no edge")
	}

	// Subtract trading costs; the remaining edge must clear the buffer.
	cost := in.HalfSpread + in.FeeRate*in.Price*(1-in.Price) + in.Slippage
	edgeEff := edge - cost
	if edgeEff <= cfg.EdgeBuffer {
		return skip("edge below buffer after costs")
	}

	// Fractional Kelly: full Kelly for a binary buy is edgeEff/(1-price).
	stake := cfg.KellyFraction * (edgeEff / (1 - in.Price)) * in.Bankroll

	// Per-bet and book-depth caps.
	if cap := cfg.PerBetFrac * in.Bankroll; stake > cap {
		stake = cap
	}
	if in.DepthCapUSD > 0 && stake > in.DepthCapUSD {
		stake = in.DepthCapUSD
	}

	// Drawdown de-risk: halve when below the high-water threshold.
	if in.DrawdownFrac >= cfg.DDDerisk {
		stake *= 0.5
	}

	// Total-exposure cap: never let open exposure exceed the bankroll fraction.
	avail := cfg.MaxExposureFrac*in.Bankroll - in.Exposure
	if avail <= 0 {
		return skip("exposure cap reached")
	}
	if stake > avail {
		stake = avail
	}

	if stake < cfg.MinStakeUSD {
		return skip("below min stake")
	}
	return Decision{Stake: stake}
}
