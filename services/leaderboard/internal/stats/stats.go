// Package stats computes per-wallet consistency metrics from data-api activity.
//
// Group activity by conditionId and accumulate net cash flow per market:
//
//	TRADE  side BUY  -> -usdcSize   (cash out: buying shares)   shares += size
//	TRADE  side SELL -> +usdcSize   (cash in:  selling shares)  shares -= size
//	REDEEM          -> +usdcSize   (cash in:  redeeming winning shares)
//	MERGE           -> +usdcSize   (cash in)
//	SPLIT           -> -usdcSize   (cash out)
//	REWARD          -> ignored
//
// A market counts as "resolved for the wallet" when it either (a) has a REDEEM
// (held to resolution and won), or (b) was opened with buys and then fully sold
// back to a ~zero net-share position (closed by exiting). Case (b) is what makes
// losing exits visible: a redeem-only rule would omit them (you don't redeem
// worthless shares), inflating the win rate. A market is a win iff net cash > 0.
//
// Limitation: a position held to a LOSING resolution (never redeemed, never sold)
// stays "open" here and is excluded — detecting it would require querying each
// market's on-chain resolution. So this is a slight upward bias, far smaller than
// the redeem-only rule's.
package stats

import (
	"math"

	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/dataapi"
)

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
	ResolvedMarkets int     // markets redeemed OR fully closed by selling out
	RealizedPnL     float64 // sum of net cash over resolved markets
	CapitalDeployed float64 // sum of USDC put in (BUY+SPLIT) over resolved markets
	ROI             float64 // RealizedPnL / CapitalDeployed; 0 when no capital deployed
	TradedMarkets   int     // distinct conditionIds seen
}

// market accumulates per-conditionId state.
type market struct {
	net          float64
	cost         float64 // USDC deployed (BUY + SPLIT) — the cost basis
	redeemed     bool    // saw at least one REDEEM (held to resolution)
	boughtShares float64 // total shares bought (opened the position)
	netShares    float64 // bought - sold; ~0 once fully exited
}

// closedShareEps treats a residual share position within this fraction of the
// shares bought as "fully exited" (tolerates rounding dust).
const closedShareEps = 1e-3

// resolved reports whether the market's outcome is realized for the wallet:
// redeemed, or opened-then-sold-back-to-flat.
func (m *market) resolved() bool {
	if m.redeemed {
		return true
	}
	return m.boughtShares > 0 && math.Abs(m.netShares) <= m.boughtShares*closedShareEps
}

// Compute derives consistency metrics from a wallet's activity.
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
				m.cost += a.USDCSize
				m.boughtShares += a.Size
				m.netShares += a.Size
			case sideSell:
				m.net += a.USDCSize
				m.netShares -= a.Size
			}
		case typeRedeem:
			m.net += a.USDCSize
			m.redeemed = true
		case typeMerge:
			m.net += a.USDCSize
		case typeSplit:
			m.net -= a.USDCSize
			m.cost += a.USDCSize
		default:
			// REWARD and any unknown types are ignored.
		}
	}

	var s Stats
	var wins int
	s.TradedMarkets = len(markets)
	for _, m := range markets {
		if !m.resolved() {
			continue
		}
		s.ResolvedMarkets++
		s.RealizedPnL += m.net
		s.CapitalDeployed += m.cost
		if m.net > 0 {
			wins++
		}
	}
	if s.ResolvedMarkets > 0 {
		s.WinRate = float64(wins) / float64(s.ResolvedMarkets)
	}
	if s.CapitalDeployed > 0 {
		// ROI = realized profit per dollar of capital deployed. This is the edge
		// metric (a 90%-win wallet buying 0.95 favorites can still have low ROI).
		s.ROI = s.RealizedPnL / s.CapitalDeployed
	}
	return s
}
