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
