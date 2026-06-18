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
// (Mirrors stats.Stats but kept local so selection stays a pure, dependency-
// free ranking package.)
type Stats struct {
	WinRate         float64
	ResolvedMarkets int
	RealizedPnL     float64
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

// Select ranks candidates by Score descending and returns up to topN wallets.
// Candidates whose stats are missing, or whose ResolvedMarkets < minResolved,
// are excluded (filters out one-hit-wonders with too little resolved history).
// Ties break deterministically by wallet ascending.
func Select(candidates []Candidate, statsByWallet map[string]Stats, minResolved, topN int) []string {
	type scored struct {
		wallet  string
		score   float64
		winRate float64
	}
	var pool []scored
	for _, c := range candidates {
		st, ok := statsByWallet[c.Wallet]
		if !ok {
			continue
		}
		if st.ResolvedMarkets < minResolved {
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
