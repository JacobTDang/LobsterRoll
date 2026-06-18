package caps

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReserve_PerTrade(t *testing.T) {
	c := New(25, 1000, 1000)
	if d := c.Reserve(30, true); d.Allowed {
		t.Fatal("expected per-trade denial for $30 > $25")
	}
	if d := c.Reserve(25, true); !d.Allowed {
		t.Fatalf("expected $25 allowed: %s", d.Reason)
	}
}

func TestReserve_PerDay(t *testing.T) {
	c := New(100, 100, 1000)
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
	c := New(100, 10000, 100)
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
	c := New(100, 100, 100000)
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
	c := New(100, 100, 100)
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

// TestReserve_Race proves the atomic check+commit never lets the committed total
// exceed the per-day cap under concurrency.
func TestReserve_Race(t *testing.T) {
	c := New(10, 100, 100000) // per-day 100, each reserve 10 -> at most 10 succeed
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
