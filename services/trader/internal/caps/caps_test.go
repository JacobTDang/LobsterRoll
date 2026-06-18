package caps

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReserve_PerTrade(t *testing.T) {
	c := New(25, 1000, 1000, nil, nil)
	if d := c.Reserve(30, true); d.Allowed {
		t.Fatal("expected per-trade denial for $30 > $25")
	}
	if d := c.Reserve(25, true); !d.Allowed {
		t.Fatalf("expected $25 allowed: %s", d.Reason)
	}
}

func TestReserve_PerDay(t *testing.T) {
	c := New(100, 100, 1000, nil, nil)
	if d := c.Reserve(60, true); !d.Allowed {
		t.Fatalf("first: %s", d.Reason)
	}
	if d := c.Reserve(50, true); d.Allowed {
		t.Fatal("expected per-day denial (60+50 > 100)")
	}
	if d := c.Reserve(40, true); !d.Allowed {
		t.Fatalf("40 should fit (60+40=100): %s", d.Reason)
	}
}

func TestReserve_OpenExposure(t *testing.T) {
	c := New(100, 10000, 100, nil, nil)
	if d := c.Reserve(70, true); !d.Allowed {
		t.Fatalf("first buy: %s", d.Reason)
	}
	if d := c.Reserve(40, true); d.Allowed {
		t.Fatal("expected exposure denial (70+40 > 100)")
	}
	// A sell reduces exposure and is not gated by the exposure cap.
	if d := c.Reserve(50, false); !d.Allowed {
		t.Fatalf("sell should be allowed: %s", d.Reason)
	}
	// Exposure now 70-50=20, so a 40 buy fits again.
	if d := c.Reserve(40, true); !d.Allowed {
		t.Fatalf("buy after sell freed exposure: %s", d.Reason)
	}
}

func TestReserve_DailyReset(t *testing.T) {
	day := time.Date(2026, 6, 17, 23, 0, 0, 0, time.UTC)
	c := New(100, 100, 100000, nil, nil)
	c.now = func() time.Time { return day }
	if d := c.Reserve(100, false); !d.Allowed { // sells don't touch exposure
		t.Fatalf("day1: %s", d.Reason)
	}
	if d := c.Reserve(1, false); d.Allowed {
		t.Fatal("day1 should be exhausted")
	}
	// Next UTC day → daily spend resets.
	c.now = func() time.Time { return day.Add(2 * time.Hour) }
	if d := c.Reserve(100, false); !d.Allowed {
		t.Fatalf("day2 after reset: %s", d.Reason)
	}
}

func TestRelease(t *testing.T) {
	c := New(100, 100, 100, nil, nil)
	c.Reserve(80, true)
	c.Release(80, true) // placement failed
	ds, oe := c.Snapshot()
	if ds != 0 || oe != 0 {
		t.Fatalf("after release daySpent=%v exposure=%v, want 0/0", ds, oe)
	}
	if d := c.Reserve(80, true); !d.Allowed {
		t.Fatalf("should fit again after release: %s", d.Reason)
	}
}

func TestReserve_NoBackwardClockReset(t *testing.T) {
	day2 := time.Date(2026, 6, 18, 1, 0, 0, 0, time.UTC)
	c := New(100, 100, 100000, nil, nil)
	c.now = func() time.Time { return day2 }
	if d := c.Reserve(100, false); !d.Allowed {
		t.Fatalf("day2: %s", d.Reason)
	}
	// Clock steps BACKWARD to the previous day — must NOT reset the daily budget.
	c.now = func() time.Time { return day2.Add(-2 * time.Hour) }
	if d := c.Reserve(1, false); d.Allowed {
		t.Fatal("backward clock step must not re-open the daily cap")
	}
}

func TestRelease_SellSymmetric(t *testing.T) {
	c := New(100, 10000, 100, nil, nil)
	// A sell from zero must produce signed (negative) exposure — a floor here
	// would clamp to 0 and make Reserve/Release asymmetric (phantom exposure).
	c.Reserve(30, false)
	if _, oe := c.Snapshot(); oe != -30 {
		t.Fatalf("exposure after sell reserve = %v, want -30 (signed, not floored)", oe)
	}
	// Release reverses it exactly back to 0.
	c.Release(30, false)
	if _, oe := c.Snapshot(); oe != 0 {
		t.Fatalf("exposure = %v after release, want 0 (symmetric)", oe)
	}
}

type fakeLedger struct {
	dk     string
	ds, oe float64
	ok     bool
	saves  int
}

func (l *fakeLedger) LoadCaps(_ context.Context) (string, float64, float64, bool, error) {
	return l.dk, l.ds, l.oe, l.ok, nil
}
func (l *fakeLedger) SaveCaps(_ context.Context, dk string, ds, oe float64) error {
	l.dk, l.ds, l.oe, l.saves = dk, ds, oe, l.saves+1
	return nil
}

func TestCaps_PersistAndReload(t *testing.T) {
	led := &fakeLedger{}
	c := New(100, 1000, 1000, led, nil)
	c.Reserve(40, true)
	if led.saves == 0 || led.ds != 40 || led.oe != 40 {
		t.Fatalf("ledger after reserve = %+v", led)
	}
	// New instance (restart) reloads the persisted ledger → caps are NOT reset.
	led.ok = true
	c2 := New(100, 1000, 1000, led, nil)
	if ds, oe := c2.Snapshot(); ds != 40 || oe != 40 {
		t.Fatalf("reloaded caps = %v/%v, want 40/40 (survive restart)", ds, oe)
	}
}

// TestReserve_Race proves the atomic check+commit never lets the committed total
// exceed the per-day cap under concurrency.
func TestReserve_Race(t *testing.T) {
	c := New(10, 100, 100000, nil, nil) // per-day 100, each reserve 10 -> at most 10 succeed
	var allowed int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c.Reserve(10, false).Allowed {
				atomic.AddInt32(&allowed, 1)
			}
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&allowed); got != 10 {
		t.Fatalf("allowed = %d, want exactly 10 (10*$10 = $100 cap)", got)
	}
	if ds, _ := c.Snapshot(); ds != 100 {
		t.Fatalf("daySpent = %v, want 100 (never exceeded)", ds)
	}
}
