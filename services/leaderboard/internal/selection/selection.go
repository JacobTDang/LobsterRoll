// Package selection ranks candidate wallets by a consistency score and picks
// the top N, excluding wallets that haven't resolved enough markets to trust.
package selection

import (
	"math"
	"sort"
)

// Candidate is a wallet from the leaderboard candidate pool.
type Candidate struct {
	Wallet   string
	Profit30D float64
}

// Stats are the per-wallet consistency metrics needed to score a candidate.
// (Mirrors stats.Stats + portfolio, kept local so selection stays a pure,
// dependency-free ranking package.)
type Stats struct {
	WinRate         float64
	ResolvedMarkets int
	RealizedPnL     float64
	PortfolioUSD    float64
}

// Criteria are the hard quality gates a wallet must clear to be tracked. A
// candidate is excluded unless it meets ALL of them.
type Criteria struct {
	MinResolved     int     // enough resolved markets for win rate to be meaningful
	MinWinRate      float64 // 0..1
	MinPortfolioUSD float64 // current portfolio value
	MinRealizedPnL  float64 // proven net profit (cash actually won)
}

// meets reports whether s clears every gate in c.
func (c Criteria) meets(s Stats) bool {
	return s.ResolvedMarkets >= c.MinResolved &&
		s.WinRate >= c.MinWinRate &&
		s.PortfolioUSD >= c.MinPortfolioUSD &&
		s.RealizedPnL >= c.MinRealizedPnL
}

// Score is winRate * log(1 + max(0, realizedPnL)). Negative realized PnL is
// clamped to 0 so a single big loss can't produce a NaN/negative score; such a
// wallet still scores 0 and loses to any profitable, accurate one.
func Score(s Stats) float64 {
	pnl := s.RealizedPnL
	if pnl < 0 {
		pnl = 0
	}
	return s.WinRate * math.Log1p(pnl)
}

// Select returns up to topN wallets that clear every gate in crit, ranked by
// Score descending. Candidates whose stats are missing, or that fail any gate,
// are excluded. With strict gates the result may be far fewer than topN — that's
// intended (quality over a fixed count). Ties break by win rate, then wallet.
func Select(candidates []Candidate, statsByWallet map[string]Stats, crit Criteria, topN int) []string {
	type scored struct {
		wallet  string
		score   float64
		winRate float64
	}
	var pool []scored
	for _, c := range candidates {
		st, ok := statsByWallet[c.Wallet]
		if !ok || !crit.meets(st) {
			continue
		}
		pool = append(pool, scored{wallet: c.Wallet, score: Score(st), winRate: st.WinRate})
	}

	// Rank by score, then win rate (so a pool of equal/zero scores — e.g. all
	// break-even — favors the more accurate wallet rather than going alphabetical),
	// then wallet for a deterministic tie-break.
	sort.Slice(pool, func(i, j int) bool {
		if pool[i].score != pool[j].score {
			return pool[i].score > pool[j].score
		}
		if pool[i].winRate != pool[j].winRate {
			return pool[i].winRate > pool[j].winRate
		}
		return pool[i].wallet < pool[j].wallet
	})

	if topN >= 0 && len(pool) > topN {
		pool = pool[:topN]
	}
	out := make([]string, len(pool))
	for i, s := range pool {
		out[i] = s.wallet
	}
	return out
}
