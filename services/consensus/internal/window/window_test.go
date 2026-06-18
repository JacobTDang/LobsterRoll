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

func openTemp(t *testing.T, win time.Duration, now func() time.Time) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "c.db"), win, now)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func trade(wallet, token, side, size, price string) bus.TradeDetected {
	return bus.TradeDetected{Wallet: wallet, TokenID: token, Side: side, Size: size, Price: price}
}

// record + ShouldFire helper that mirrors the aggregator's use of the store.
func recordFire(t *testing.T, s *Store, ev bus.TradeDetected) (Cohort, bool) {
	t.Helper()
	ctx := context.Background()
	c, err := s.Record(ctx, ev)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	fired, err := s.ShouldFire(ctx, ev.TokenID, ev.Side, c.Count())
	if err != nil {
		t.Fatalf("ShouldFire: %v", err)
	}
	return c, fired
}

func TestConsensus_FiresOnceThenOnGrowth(t *testing.T) {
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	s := openTemp(t, 6*time.Hour, clk.now)

	// Threshold is enforced by the aggregator; here we treat "fires at count>=3"
	// by asserting fired flags only matter once count crosses 3.
	const tok, side = "TOK", "buy"

	// wallet A establishes the cohort; raw fire semantics are covered in
	// TestShouldFire_Growth. Here we assert growth/dedup via cohort counts.
	recordFire(t, s, trade("0xA", tok, side, "10", "0.5"))

	c, _ := recordFire(t, s, trade("0xB", tok, side, "10", "0.5"))
	if c.Count() != 2 {
		t.Fatalf("after A,B count=%d want 2", c.Count())
	}

	// 3rd distinct wallet -> cohort of 3.
	c, _ = recordFire(t, s, trade("0xC", tok, side, "10", "0.5"))
	if c.Count() != 3 {
		t.Fatalf("after A,B,C count=%d want 3", c.Count())
	}

	// Same wallet A trading repeatedly does NOT increase the distinct count.
	c, _ = recordFire(t, s, trade("0xA", tok, side, "10", "0.5"))
	if c.Count() != 3 {
		t.Fatalf("A repeat count=%d want 3 (no false growth)", c.Count())
	}
	c, _ = recordFire(t, s, trade("0xa", tok, side, "10", "0.5")) // case-insensitive
	if c.Count() != 3 {
		t.Fatalf("A repeat (mixed case) count=%d want 3", c.Count())
	}

	// 4th distinct wallet -> cohort grows to 4.
	c, _ = recordFire(t, s, trade("0xD", tok, side, "10", "0.5"))
	if c.Count() != 4 {
		t.Fatalf("after D count=%d want 4", c.Count())
	}
}

func TestShouldFire_Growth(t *testing.T) {
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	s := openTemp(t, 6*time.Hour, clk.now)
	ctx := context.Background()
	const tok, side = "TOK", "buy"

	// Fire at first reach of cohort 3.
	ok, err := s.ShouldFire(ctx, tok, side, 3)
	if err != nil || !ok {
		t.Fatalf("first reach of 3: ok=%v err=%v want true", ok, err)
	}
	// Same cohort size again => no fire.
	if ok, _ := s.ShouldFire(ctx, tok, side, 3); ok {
		t.Fatalf("repeat at 3 should not fire")
	}
	// Smaller (pruned) cohort => no fire.
	if ok, _ := s.ShouldFire(ctx, tok, side, 2); ok {
		t.Fatalf("count drop to 2 should not fire")
	}
	// Growth to 4 => fire.
	if ok, _ := s.ShouldFire(ctx, tok, side, 4); !ok {
		t.Fatalf("growth to 4 should fire")
	}
	// 4 again => no fire.
	if ok, _ := s.ShouldFire(ctx, tok, side, 4); ok {
		t.Fatalf("repeat at 4 should not fire")
	}
}

func TestConsensus_SidesTrackedSeparately(t *testing.T) {
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	s := openTemp(t, 6*time.Hour, clk.now)
	const tok = "TOK"

	recordFire(t, s, trade("0xA", tok, "buy", "1", "1"))
	recordFire(t, s, trade("0xB", tok, "buy", "1", "1"))
	cBuy, _ := recordFire(t, s, trade("0xC", tok, "buy", "1", "1"))
	if cBuy.Count() != 3 {
		t.Fatalf("buy cohort=%d want 3", cBuy.Count())
	}
	cSell, _ := recordFire(t, s, trade("0xA", tok, "sell", "1", "1"))
	if cSell.Count() != 1 {
		t.Fatalf("sell cohort=%d want 1 (separate from buy)", cSell.Count())
	}
}

func TestConsensus_OldTradesPruned(t *testing.T) {
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	s := openTemp(t, 6*time.Hour, clk.now)
	const tok, side = "TOK", "buy"

	recordFire(t, s, trade("0xA", tok, side, "1", "1"))
	recordFire(t, s, trade("0xB", tok, side, "1", "1"))

	// Advance past the window so A and B fall out.
	clk.advance(7 * time.Hour)

	c, _ := recordFire(t, s, trade("0xC", tok, side, "1", "1"))
	if c.Count() != 1 {
		t.Fatalf("after prune count=%d want 1 (only C in window)", c.Count())
	}
	// Confirm A is gone: re-adding C-era wallet D yields 2, not 3+.
	c, _ = recordFire(t, s, trade("0xD", tok, side, "1", "1"))
	if c.Count() != 2 {
		t.Fatalf("count=%d want 2 (C,D), pruned A,B", c.Count())
	}
}

func TestConsensus_CombinedUSD(t *testing.T) {
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	s := openTemp(t, 6*time.Hour, clk.now)
	const tok, side = "TOK", "buy"

	recordFire(t, s, trade("0xA", tok, side, "10", "0.5")) // 5
	recordFire(t, s, trade("0xB", tok, side, "20", "0.25")) // 5
	c, _ := recordFire(t, s, trade("0xC", tok, side, "4", "1")) // 4
	want := 5.0 + 5.0 + 4.0
	if c.CombinedUSD < want-1e-9 || c.CombinedUSD > want+1e-9 {
		t.Fatalf("CombinedUSD=%v want %v", c.CombinedUSD, want)
	}
}

func TestConsensus_UnparseableUSDIsZero(t *testing.T) {
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	s := openTemp(t, 6*time.Hour, clk.now)
	c, _ := recordFire(t, s, trade("0xA", "TOK", "buy", "abc", "0.5"))
	if c.CombinedUSD != 0 {
		t.Fatalf("CombinedUSD=%v want 0 for unparseable size", c.CombinedUSD)
	}
}

func TestStore_ConcurrentRecord(t *testing.T) {
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	s := openTemp(t, 6*time.Hour, clk.now)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ev := trade("0xWALLET", "TOK", "buy", "1", "1")
			if _, err := s.Record(ctx, ev); err != nil {
				t.Errorf("Record: %v", err)
			}
			if _, err := s.ShouldFire(ctx, "TOK", "buy", 1); err != nil {
				t.Errorf("ShouldFire: %v", err)
			}
		}(i)
	}
	wg.Wait()
}
