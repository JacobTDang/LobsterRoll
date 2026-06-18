package clv

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestCLV_BuyBeatsCloseWhenPriceRises(t *testing.T) {
	// Bought YES at 0.40, market closed near 0.55 -> got in cheap, +0.15 CLV.
	if got := CLV(0.40, 0.55, true); !approx(got, 0.15) {
		t.Errorf("CLV buy = %v, want 0.15", got)
	}
	// Bought at 0.60, closed 0.50 -> entered too high, negative CLV.
	if got := CLV(0.60, 0.50, true); !approx(got, -0.10) {
		t.Errorf("CLV buy = %v, want -0.10", got)
	}
}

func TestCLV_SellIsInverted(t *testing.T) {
	// Sold at 0.60, closed 0.45 -> price fell as wanted, +0.15 CLV.
	if got := CLV(0.60, 0.45, false); !approx(got, 0.15) {
		t.Errorf("CLV sell = %v, want 0.15", got)
	}
	// Sold at 0.40, closed 0.50 -> price rose against the sell, negative CLV.
	if got := CLV(0.40, 0.50, false); !approx(got, -0.10) {
		t.Errorf("CLV sell = %v, want -0.10", got)
	}
}

func TestAverageAndBeatRate(t *testing.T) {
	clvs := []float64{0.10, -0.05, 0.20, 0.0}
	if got := Average(clvs); !approx(got, 0.0625) {
		t.Errorf("Average = %v, want 0.0625", got)
	}
	// 0.0 is not strictly positive -> 2 of 4 beat.
	if got := BeatRate(clvs); !approx(got, 0.5) {
		t.Errorf("BeatRate = %v, want 0.5", got)
	}
	if Average(nil) != 0 || BeatRate(nil) != 0 {
		t.Error("empty set should yield 0")
	}
}
