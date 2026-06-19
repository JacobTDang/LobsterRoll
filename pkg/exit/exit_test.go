package exit

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func base() Position {
	return Position{EntryPrice: 0.40, CurPrice: 0.55, OppPrice: 0.50, Shares: 100}
}

func cfg() Config {
	return Config{StopLoss: 0.25, TakeProfit: 0.70, TakeProfitFrac: 0.5, HedgeLockMin: 0.05, LeaderExitFrac: 1.0}
}

func TestDecide_Hold(t *testing.T) {
	// Cur 0.55: above stop, below take-profit; opp 0.50 -> lock frac = 0.10 >= 0.05.
	// But raise HedgeLockMin so nothing triggers -> Hold.
	c := cfg()
	c.HedgeLockMin = 0.20
	if a := Decide(base(), c); a.Kind != Hold {
		t.Fatalf("got %+v, want Hold", a)
	}
}

func TestDecide_StopLossBeatsAll(t *testing.T) {
	p := base()
	p.CurPrice = 0.20 // <= stop 0.25
	c := cfg()
	c.LeaderExited = true // stop-loss must still win
	a := Decide(p, c)
	if a.Kind != Sell || !approx(a.Shares, 100) || a.Reason != "stop loss" {
		t.Fatalf("got %+v, want Sell all / stop loss", a)
	}
}

func TestDecide_StopLossInclusiveBoundary(t *testing.T) {
	p := base()
	p.CurPrice = 0.25 // exactly AT the stop -> must sell (boundary inclusive)
	if a := Decide(p, cfg()); a.Kind != Sell || a.Reason != "stop loss" {
		t.Fatalf("at-stop: got %+v, want Sell / stop loss", a)
	}
}

func TestDecide_LeaderExit(t *testing.T) {
	c := cfg()
	c.LeaderExited = true
	c.LeaderExitFrac = 0.5
	a := Decide(base(), c)
	if a.Kind != Sell || !approx(a.Shares, 50) || a.Reason != "leader exited" {
		t.Fatalf("got %+v, want Sell 50 / leader exited", a)
	}
}

func TestDecide_HedgeLock(t *testing.T) {
	// entry 0.40 + opp 0.50 = 0.90 -> lockable 0.10 >= HedgeLockMin 0.05 -> hedge.
	a := Decide(base(), cfg())
	if a.Kind != Hedge || !approx(a.Shares, 100) || a.Reason != "lock profit (hedge)" {
		t.Fatalf("got %+v, want Hedge 100 / lock profit", a)
	}
	// No lock when the two sides sum to >= 1.
	p := base()
	p.OppPrice = 0.65 // 0.40+0.65=1.05 -> lockable negative
	c := cfg()
	c.TakeProfit = 0 // disable take-profit so we land on Hold
	if a := Decide(p, c); a.Kind != Hold {
		t.Fatalf("got %+v, want Hold (no lockable profit)", a)
	}
}

func TestDecide_TakeProfit(t *testing.T) {
	p := base()
	p.CurPrice = 0.75 // >= take-profit 0.70
	c := cfg()
	c.HedgeLockMin = 0 // disable hedge so take-profit is reached
	a := Decide(p, c)
	if a.Kind != Sell || !approx(a.Shares, 50) || a.Reason != "take profit" {
		t.Fatalf("got %+v, want Sell 50 / take profit", a)
	}
}

func TestDecide_NoPosition(t *testing.T) {
	p := base()
	p.Shares = 0
	if a := Decide(p, cfg()); a.Kind != Hold || a.Reason != "no position" {
		t.Fatalf("got %+v, want Hold / no position", a)
	}
}

func TestClampFrac(t *testing.T) {
	// An unset or invalid fraction must default to a FULL exit, never a silent no-op.
	c := cfg()
	c.LeaderExited = true
	c.LeaderExitFrac = 0 // invalid -> 1.0
	if a := Decide(base(), c); !approx(a.Shares, 100) {
		t.Fatalf("zero frac: shares=%v, want full 100", a.Shares)
	}
	c.LeaderExitFrac = 1.5 // out of range -> 1.0
	if a := Decide(base(), c); !approx(a.Shares, 100) {
		t.Fatalf("oversized frac: shares=%v, want full 100", a.Shares)
	}
}

func TestLockableProfitFrac(t *testing.T) {
	if got := LockableProfitFrac(0.40, 0.50); !approx(got, 0.10) {
		t.Fatalf("lockable = %v, want 0.10", got)
	}
}
