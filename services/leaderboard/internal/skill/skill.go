// Package skill turns raw per-wallet ROI into a skill estimate that separates
// proven edge from small-sample luck.
//
// It applies credibility (empirical-Bayes-style) shrinkage: each wallet's ROI is
// pulled toward the population mean by an amount that shrinks as its sample size
// grows. A wallet with a huge ROI over 5 resolved markets is mostly luck and gets
// pulled hard to the mean; a wallet with a solid ROI over 300 markets barely
// moves. Ranking by the shrunk ROI therefore favors demonstrated skill over
// flukes — the direct guard against survivorship bias on a winners' leaderboard.
package skill

// Input is a wallet's raw skill inputs.
type Input struct {
	Wallet string
	ROI    float64 // realized profit / capital deployed
	N      int     // resolved markets (sample size)
}

// Result is a wallet's shrunk ROI and a 0–100 skill score (its percentile rank
// by shrunk ROI within the scored population).
type Result struct {
	Wallet    string
	ShrunkROI float64
	Score     int
}

// Shrink computes shrunk ROI + skill score for every wallet in pop. k is the
// prior strength in "equivalent resolved markets": a wallet needs ~k resolved
// markets before its own ROI outweighs the population prior
// (shrunk = (n·ROI + k·μ)/(n + k)). μ is the sample-size-weighted population mean
// ROI. Returns results in the same order as pop.
func Shrink(pop []Input, k float64) []Result {
	if len(pop) == 0 {
		return nil
	}
	if k <= 0 {
		k = 1
	}

	// Population mean ROI, weighted by sample size (big-sample wallets anchor the
	// prior more than noisy small-sample ones). This is an IN-SAMPLE mean — each
	// wallet's own ROI is included in the prior it's shrunk toward. That's standard
	// empirical-Bayes practice and only matters when a single wallet's N dwarfs the
	// rest of the pool; a strict leave-one-out prior is unnecessary here.
	var sumN, sumNROI float64
	for _, w := range pop {
		n := float64(w.N)
		sumN += n
		sumNROI += n * w.ROI
	}
	mu := 0.0
	if sumN > 0 {
		mu = sumNROI / sumN
	}

	res := make([]Result, len(pop))
	for i, w := range pop {
		n := float64(w.N)
		res[i] = Result{Wallet: w.Wallet, ShrunkROI: (n*w.ROI + k*mu) / (n + k)}
	}
	assignScores(res)
	return res
}

// assignScores sets Score as the 0–100 percentile rank by ShrunkROI (highest
// shrunk ROI = 100, lowest = 0, single wallet = 100). Rank is the count of
// wallets with a STRICTLY smaller ShrunkROI, so equally-skilled wallets get the
// SAME score (ranking by array index would spread ties across the whole range,
// e.g. five identical wallets shown as 0/25/50/75/100).
func assignScores(res []Result) {
	n := len(res)
	if n == 1 {
		res[0].Score = 100
		return
	}
	for i := range res {
		smaller := 0
		for j := range res {
			if res[j].ShrunkROI < res[i].ShrunkROI {
				smaller++
			}
		}
		res[i].Score = int(float64(smaller)/float64(n-1)*100 + 0.5)
	}
}
