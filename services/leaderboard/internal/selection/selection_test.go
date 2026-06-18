package selection

import (
	"reflect"
	"testing"
)

func TestSelect_RanksByScore(t *testing.T) {
	cands := []Candidate{
		{Wallet: "0xlow"}, {Wallet: "0xhigh"}, {Wallet: "0xmid"},
	}
	stats := map[string]Stats{
		// score = winRate * log1p(pnl)
		"0xhigh": {WinRate: 0.9, ResolvedMarkets: 50, RealizedPnL: 1_000_000}, // ~12.4
		"0xmid":  {WinRate: 0.6, ResolvedMarkets: 50, RealizedPnL: 100_000},   // ~6.9
		"0xlow":  {WinRate: 0.5, ResolvedMarkets: 50, RealizedPnL: 1_000},     // ~3.45
	}
	got := Select(cands, stats, Criteria{MinResolved: 20}, 10)
	want := []string{"0xhigh", "0xmid", "0xlow"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Select = %v, want %v", got, want)
	}
}

func TestSelect_ExcludesOneHitWonder(t *testing.T) {
	cands := []Candidate{{Wallet: "0xpro"}, {Wallet: "0xlucky"}}
	stats := map[string]Stats{
		// Lucky has a monster pnl but only 1 resolved market: excluded by minResolved.
		"0xlucky": {WinRate: 1.0, ResolvedMarkets: 1, RealizedPnL: 10_000_000},
		"0xpro":   {WinRate: 0.7, ResolvedMarkets: 40, RealizedPnL: 500_000},
	}
	got := Select(cands, stats, Criteria{MinResolved: 20}, 10)
	want := []string{"0xpro"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Select = %v, want %v (one-hit-wonder must be excluded)", got, want)
	}
}

func TestSelect_MissingStatsExcluded(t *testing.T) {
	cands := []Candidate{{Wallet: "0xa"}, {Wallet: "0xb"}}
	stats := map[string]Stats{
		"0xa": {WinRate: 0.8, ResolvedMarkets: 30, RealizedPnL: 1000},
		// 0xb has no stats -> excluded.
	}
	got := Select(cands, stats, Criteria{MinResolved: 20}, 10)
	want := []string{"0xa"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Select = %v, want %v", got, want)
	}
}

func TestSelect_TopNTruncates(t *testing.T) {
	cands := []Candidate{{Wallet: "0xa"}, {Wallet: "0xb"}, {Wallet: "0xc"}}
	stats := map[string]Stats{
		"0xa": {WinRate: 0.9, ResolvedMarkets: 30, RealizedPnL: 1_000_000},
		"0xb": {WinRate: 0.8, ResolvedMarkets: 30, RealizedPnL: 1_000_000},
		"0xc": {WinRate: 0.7, ResolvedMarkets: 30, RealizedPnL: 1_000_000},
	}
	got := Select(cands, stats, Criteria{MinResolved: 20}, 2)
	want := []string{"0xa", "0xb"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Select = %v, want %v", got, want)
	}
}

func TestSelect_ZeroScoreBreaksByWinRate(t *testing.T) {
	// All break-even (pnl 0 -> score 0); the higher win rate must rank first,
	// not the alphabetically-first wallet.
	cands := []Candidate{{Wallet: "0xaaa"}, {Wallet: "0xzzz"}}
	stats := map[string]Stats{
		"0xaaa": {WinRate: 0.3, ResolvedMarkets: 30, RealizedPnL: 0},
		"0xzzz": {WinRate: 0.9, ResolvedMarkets: 30, RealizedPnL: 0},
	}
	got := Select(cands, stats, Criteria{MinResolved: 20}, 10)
	want := []string{"0xzzz", "0xaaa"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Select = %v, want %v (zero-score ties break by win rate)", got, want)
	}
}

func TestSelect_DeterministicTieBreak(t *testing.T) {
	cands := []Candidate{{Wallet: "0xc"}, {Wallet: "0xa"}, {Wallet: "0xb"}}
	// Identical stats -> identical scores; tie-break is wallet ascending.
	st := Stats{WinRate: 0.7, ResolvedMarkets: 30, RealizedPnL: 50_000}
	stats := map[string]Stats{"0xa": st, "0xb": st, "0xc": st}
	got := Select(cands, stats, Criteria{MinResolved: 20}, 10)
	want := []string{"0xa", "0xb", "0xc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Select = %v, want %v (deterministic tie-break)", got, want)
	}
}

func TestSelect_RanksByShrunkROI(t *testing.T) {
	cands := []Candidate{{Wallet: "0xloser"}, {Wallet: "0xwinner"}}
	stats := map[string]Stats{
		// Higher win rate but a NEGATIVE skill-adjusted ROI...
		"0xloser": {WinRate: 0.5, ResolvedMarkets: 30, RealizedPnL: -100_000, ShrunkROI: -0.20},
		// ...loses to the lower-win-rate wallet whose shrunk ROI is positive.
		"0xwinner": {WinRate: 0.4, ResolvedMarkets: 30, RealizedPnL: 10, ShrunkROI: 0.15},
	}
	got := Select(cands, stats, Criteria{MinResolved: 20, MinRealizedPnL: -1_000_000}, 10)
	want := []string{"0xwinner", "0xloser"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Select = %v, want %v (rank by shrunk ROI, not win rate)", got, want)
	}
}

func TestSelect_Empty(t *testing.T) {
	got := Select(nil, map[string]Stats{}, Criteria{MinResolved: 20}, 10)
	if len(got) != 0 {
		t.Errorf("Select(nil) = %v, want empty", got)
	}
}

func TestSelect_GatesWinRatePortfolioRealized(t *testing.T) {
	cands := []Candidate{
		{Wallet: "0xelite"}, {Wallet: "0xlowwin"}, {Wallet: "0xbroke"}, {Wallet: "0xunprofitable"},
	}
	stats := map[string]Stats{
		"0xelite":        {WinRate: 0.95, ResolvedMarkets: 30, RealizedPnL: 500_000, PortfolioUSD: 1_000_000},
		"0xlowwin":       {WinRate: 0.85, ResolvedMarkets: 30, RealizedPnL: 500_000, PortfolioUSD: 1_000_000}, // win < 0.90
		"0xbroke":        {WinRate: 0.99, ResolvedMarkets: 30, RealizedPnL: 500_000, PortfolioUSD: 50_000},    // portfolio < 100k
		"0xunprofitable": {WinRate: 0.99, ResolvedMarkets: 30, RealizedPnL: -10, PortfolioUSD: 1_000_000},     // realized < 0
	}
	crit := Criteria{MinResolved: 20, MinWinRate: 0.90, MinPortfolioUSD: 100_000, MinRealizedPnL: 0}
	got := Select(cands, stats, crit, 10)
	want := []string{"0xelite"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Select = %v, want %v (only the wallet clearing every gate)", got, want)
	}
}

func TestSelect_RequireFreshExcludesCooling(t *testing.T) {
	cands := []Candidate{{Wallet: "0xfresh"}, {Wallet: "0xcooling"}}
	stats := map[string]Stats{
		"0xfresh":   {WinRate: 0.95, ResolvedMarkets: 30, RealizedPnL: 500_000, PortfolioUSD: 1_000_000, ShrunkROI: 0.30, Fresh: true},
		"0xcooling": {WinRate: 0.95, ResolvedMarkets: 30, RealizedPnL: 500_000, PortfolioUSD: 1_000_000, ShrunkROI: 0.40, Fresh: false},
	}
	crit := Criteria{MinResolved: 20, MinWinRate: 0.90, MinPortfolioUSD: 100_000, RequireFresh: true}
	got := Select(cands, stats, crit, 10)
	// Cooling wallet excluded even though its shrunk ROI is higher.
	if len(got) != 1 || got[0] != "0xfresh" {
		t.Fatalf("Select = %v, want [0xfresh] (cooling wallet gated out)", got)
	}

	// With RequireFresh off, the cooling (higher-ROI) wallet is kept and ranks first.
	crit.RequireFresh = false
	got = Select(cands, stats, crit, 10)
	if len(got) != 2 || got[0] != "0xcooling" {
		t.Fatalf("Select = %v, want [0xcooling 0xfresh] when fresh not required", got)
	}
}

func TestSelect_StrictMayReturnFewerThanTopN(t *testing.T) {
	// Strict gates: even with topN=30 and many candidates, only those clearing
	// the bar are returned (quality over a fixed count).
	cands := []Candidate{{Wallet: "0xa"}, {Wallet: "0xb"}, {Wallet: "0xc"}}
	stats := map[string]Stats{
		"0xa": {WinRate: 0.95, ResolvedMarkets: 25, RealizedPnL: 200_000, PortfolioUSD: 300_000},
		"0xb": {WinRate: 0.40, ResolvedMarkets: 25, RealizedPnL: 200_000, PortfolioUSD: 300_000},
		"0xc": {WinRate: 0.50, ResolvedMarkets: 25, RealizedPnL: 200_000, PortfolioUSD: 300_000},
	}
	got := Select(cands, stats, Criteria{MinResolved: 20, MinWinRate: 0.90, MinPortfolioUSD: 100_000}, 30)
	if len(got) != 1 || got[0] != "0xa" {
		t.Fatalf("Select = %v, want [0xa] (strict gate, fewer than topN is fine)", got)
	}
}
