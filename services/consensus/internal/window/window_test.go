package window

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
)

// clock is a controllable time source for deterministic window tests.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func openTemp(t *testing.T, win time.Duration, minWallets int, now func() time.Time) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "c.db"), win, minWallets, now)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func trade(wallet, token, side, size, price string) bus.TradeDetected {
	return bus.TradeDetected{Wallet: wallet, TokenID: token, Side: side, Size: size, Price: price}
}

// rec records a trade and returns the cohort + whether a signal fired.
func rec(t *testing.T, s *Store, ev bus.TradeDetected) (Cohort, bool) {
	t.Helper()
	c, fire, err := s.Record(context.Background(), ev)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	return c, fire
}

func TestConsensus_FiresOnceThenOnGrowth(t *testing.T) {
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	s := openTemp(t, 6*time.Hour, 3, clk.now)
	const tok, side = "TOK", "buy"

	if c, fire := rec(t, s, trade("0xA", tok, side, "10", "0.5")); c.Count() != 1 || fire {
		t.Fatalf("A: count=%d fire=%v, want 1/false", c.Count(), fire)
	}
	if c, fire := rec(t, s, trade("0xB", tok, side, "10", "0.5")); c.Count() != 2 || fire {
		t.Fatalf("B: count=%d fire=%v, want 2/false", c.Count(), fire)
	}
	// 3rd distinct wallet reaches the threshold -> fire once.
	if c, fire := rec(t, s, trade("0xC", tok, side, "10", "0.5")); c.Count() != 3 || !fire {
		t.Fatalf("C: count=%d fire=%v, want 3/true", c.Count(), fire)
	}
	// Same wallet repeating must NOT grow the cohort or re-fire.
	if c, fire := rec(t, s, trade("0xA", tok, side, "10", "0.5")); c.Count() != 3 || fire {
		t.Fatalf("A-repeat: count=%d fire=%v, want 3/false", c.Count(), fire)
	}
	if c, fire := rec(t, s, trade("0xa", tok, side, "10", "0.5")); c.Count() != 3 || fire { // mixed case
		t.Fatalf("a-repeat: count=%d fire=%v, want 3/false", c.Count(), fire)
	}
	// 4th distinct wallet -> growth to a new max -> fire again.
	if c, fire := rec(t, s, trade("0xD", tok, side, "10", "0.5")); c.Count() != 4 || !fire {
		t.Fatalf("D: count=%d fire=%v, want 4/true", c.Count(), fire)
	}
}

// The key regression test: a cohort that fires, fully ages out, then re-forms
// must fire AGAIN (the old high-water must not suppress it forever).
func TestConsensus_RefiresAfterDissipation(t *testing.T) {
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	s := openTemp(t, 6*time.Hour, 3, clk.now)
	const tok, side = "TOK", "buy"

	rec(t, s, trade("0xA", tok, side, "1", "1"))
	rec(t, s, trade("0xB", tok, side, "1", "1"))
	if _, fire := rec(t, s, trade("0xC", tok, side, "1", "1")); !fire {
		t.Fatal("first cohort of 3 should fire")
	}

	// Whole cohort ages out.
	clk.advance(7 * time.Hour)

	// A brand-new cohort of 3 distinct wallets forms.
	if c, fire := rec(t, s, trade("0xD", tok, side, "1", "1")); c.Count() != 1 || fire {
		t.Fatalf("D after dissipation: count=%d fire=%v, want 1/false", c.Count(), fire)
	}
	rec(t, s, trade("0xE", tok, side, "1", "1"))
	if c, fire := rec(t, s, trade("0xF", tok, side, "1", "1")); c.Count() != 3 || !fire {
		t.Fatalf("re-formed cohort: count=%d fire=%v, want 3/true (must re-fire)", c.Count(), fire)
	}
}

func TestConsensus_SidesTrackedSeparately(t *testing.T) {
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	s := openTemp(t, 6*time.Hour, 3, clk.now)
	const tok = "TOK"

	rec(t, s, trade("0xA", tok, "buy", "1", "1"))
	rec(t, s, trade("0xB", tok, "buy", "1", "1"))
	if cBuy, fire := rec(t, s, trade("0xC", tok, "buy", "1", "1")); cBuy.Count() != 3 || !fire {
		t.Fatalf("buy cohort=%d fire=%v, want 3/true", cBuy.Count(), fire)
	}
	// A sell on the same token is a separate cohort (count 1, no fire).
	if cSell, fire := rec(t, s, trade("0xA", tok, "sell", "1", "1")); cSell.Count() != 1 || fire {
		t.Fatalf("sell cohort=%d fire=%v, want 1/false (separate from buy)", cSell.Count(), fire)
	}
}

func TestConsensus_OldTradesPruned(t *testing.T) {
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	s := openTemp(t, 6*time.Hour, 3, clk.now)
	const tok, side = "TOK", "buy"

	rec(t, s, trade("0xA", tok, side, "1", "1"))
	rec(t, s, trade("0xB", tok, side, "1", "1"))
	clk.advance(7 * time.Hour) // A and B fall out of the window

	if c, _ := rec(t, s, trade("0xC", tok, side, "1", "1")); c.Count() != 1 {
		t.Fatalf("after prune count=%d want 1 (only C in window)", c.Count())
	}
	if c, _ := rec(t, s, trade("0xD", tok, side, "1", "1")); c.Count() != 2 {
		t.Fatalf("count=%d want 2 (C,D), pruned A,B", c.Count())
	}
}

func TestConsensus_CombinedUSD(t *testing.T) {
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	s := openTemp(t, 6*time.Hour, 3, clk.now)
	const tok, side = "TOK", "buy"

	rec(t, s, trade("0xA", tok, side, "10", "0.5"))  // 5
	rec(t, s, trade("0xB", tok, side, "20", "0.25")) // 5
	c, _ := rec(t, s, trade("0xC", tok, side, "4", "1")) // 4
	want := 5.0 + 5.0 + 4.0
	if c.CombinedUSD < want-1e-9 || c.CombinedUSD > want+1e-9 {
		t.Fatalf("CombinedUSD=%v want %v", c.CombinedUSD, want)
	}
}

func TestConsensus_UnparseableUSDIsZero(t *testing.T) {
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	s := openTemp(t, 6*time.Hour, 3, clk.now)
	c, _ := rec(t, s, trade("0xA", "TOK", "buy", "abc", "0.5"))
	if c.CombinedUSD != 0 {
		t.Fatalf("CombinedUSD=%v want 0 for unparseable size", c.CombinedUSD)
	}
}

func TestStore_ConcurrentRecord(t *testing.T) {
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	s := openTemp(t, 6*time.Hour, 3, clk.now)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := s.Record(ctx, trade("0xWALLET", "TOK", "buy", "1", "1")); err != nil {
				t.Errorf("Record: %v", err)
			}
		}()
	}
	wg.Wait()
}
