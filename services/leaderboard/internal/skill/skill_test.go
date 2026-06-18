package skill

import (
	"math"
	"testing"
)

func find(res []Result, wallet string) Result {
	for _, r := range res {
		if r.Wallet == wallet {
			return r
		}
	}
	return Result{}
}

func TestShrink_PullsSmallSampleTowardMean(t *testing.T) {
	// A lucky tiny-sample wallet (500% ROI over 2 markets) vs a proven one (+30%
	// over 1000). After shrinkage the proven wallet must outrank the fluke.
	pop := []Input{
		{Wallet: "0xlucky", ROI: 5.0, N: 2},
		{Wallet: "0xproven", ROI: 0.30, N: 1000},
		{Wallet: "0xanchor", ROI: 0.0, N: 2000},
	}
	res := Shrink(pop, 200)

	lucky := find(res, "0xlucky")
	proven := find(res, "0xproven")
	// The fluke's 500% ROI is crushed toward the mean...
	if lucky.ShrunkROI > 0.3 {
		t.Errorf("lucky shrunk ROI = %.3f, expected heavy pull toward mean", lucky.ShrunkROI)
	}
	// ...so the proven wallet ranks higher.
	if proven.ShrunkROI <= lucky.ShrunkROI {
		t.Errorf("proven (%.3f) should outrank lucky (%.3f) after shrinkage",
			proven.ShrunkROI, lucky.ShrunkROI)
	}
	if proven.Score < lucky.Score {
		t.Errorf("proven score %d should be >= lucky score %d", proven.Score, lucky.Score)
	}
}

func TestShrink_LargeSampleBarelyMoves(t *testing.T) {
	pop := []Input{
		{Wallet: "0xbig", ROI: 0.40, N: 10_000},
		{Wallet: "0xsmall", ROI: 0.40, N: 10},
		{Wallet: "0xanchor", ROI: 0.0, N: 1_000}, // pulls the population mean below 0.40
	}
	res := Shrink(pop, 100)
	big := find(res, "0xbig")
	small := find(res, "0xsmall")
	// Same raw ROI, but the big-sample wallet keeps far more of it.
	if math.Abs(big.ShrunkROI-0.40) > 0.02 {
		t.Errorf("big-sample shrunk ROI = %.3f, want ~0.40", big.ShrunkROI)
	}
	if small.ShrunkROI >= big.ShrunkROI {
		t.Errorf("small-sample (%.3f) should be pulled below big-sample (%.3f)",
			small.ShrunkROI, big.ShrunkROI)
	}
}

func TestShrink_Scores0to100(t *testing.T) {
	pop := []Input{
		{Wallet: "a", ROI: 0.1, N: 100},
		{Wallet: "b", ROI: 0.5, N: 100},
		{Wallet: "c", ROI: 0.3, N: 100},
	}
	res := Shrink(pop, 50)
	if find(res, "b").Score != 100 {
		t.Errorf("top wallet score = %d, want 100", find(res, "b").Score)
	}
	if find(res, "a").Score != 0 {
		t.Errorf("bottom wallet score = %d, want 0", find(res, "a").Score)
	}
}

func TestShrink_Empty(t *testing.T) {
	if Shrink(nil, 100) != nil {
		t.Error("Shrink(nil) should be nil")
	}
}
