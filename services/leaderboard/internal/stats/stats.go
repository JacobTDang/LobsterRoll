// Package stats computes per-wallet consistency metrics from data-api activity.
//
// Algorithm (verified against Fredi9999 = 65% over 29 resolved markets):
// group activity by conditionId and accumulate net cash flow per market:
//
//	TRADE  side BUY  -> -usdcSize   (cash out: buying shares)
//	TRADE  side SELL -> +usdcSize   (cash in:  selling shares)
//	REDEEM          -> +usdcSize   (cash in:  redeeming winning shares)
//	MERGE           -> +usdcSize   (cash in)
//	SPLIT           -> -usdcSize   (cash out)
//	REWARD          -> ignored
//
// A market is "resolved for the wallet" iff it has at least one REDEEM event.
// A market is a win iff its net cash flow > 0. WinRate = wins / resolvedMarkets,
// RealizedPnL = sum of net cash over resolved markets, TradedMarkets = distinct
// conditionIds seen.
package stats

import "github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/dataapi"

// Activity event types and trade sides.
const (
	typeTrade  = "TRADE"
	typeRedeem = "REDEEM"
	typeMerge  = "MERGE"
	typeSplit  = "SPLIT"

	sideBuy  = "BUY"
	sideSell = "SELL"
)

// Stats are the consistency metrics for a single wallet.
type Stats struct {
	WinRate         float64 // wins / resolvedMarkets, 0 when no resolved markets
	ResolvedMarkets int     // markets with at least one REDEEM
	RealizedPnL     float64 // sum of net cash over resolved markets
	TradedMarkets   int     // distinct conditionIds seen
}

// market accumulates per-conditionId state.
type market struct {
	net      float64
	resolved bool // saw at least one REDEEM
}

// Compute applies the verified win-rate algorithm to a wallet's activity.
func Compute(acts []dataapi.Activity) Stats {
	markets := make(map[string]*market)
	for _, a := range acts {
		m := markets[a.ConditionID]
		if m == nil {
			m = &market{}
			markets[a.ConditionID] = m
		}
		switch a.Type {
		case typeTrade:
			switch a.Side {
			case sideBuy:
				m.net -= a.USDCSize
			case sideSell:
				m.net += a.USDCSize
			}
		case typeRedeem:
			m.net += a.USDCSize
			m.resolved = true
		case typeMerge:
			m.net += a.USDCSize
		case typeSplit:
			m.net -= a.USDCSize
		default:
			// REWARD and any unknown types are ignored.
		}
	}

	var s Stats
	s.TradedMarkets = len(markets)
	for _, m := range markets {
		if !m.resolved {
			continue
		}
		s.ResolvedMarkets++
		s.RealizedPnL += m.net
		if m.net > 0 {
			s.WinRate++ // count wins here; normalized below
		}
	}
	if s.ResolvedMarkets > 0 {
		s.WinRate /= float64(s.ResolvedMarkets)
	} else {
		s.WinRate = 0
	}
	return s
}
