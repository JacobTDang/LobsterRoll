package aggregator

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/services/consensus/internal/window"
)

// fakePub captures published consensus signals.
type fakePub struct {
	mu   sync.Mutex
	sigs []bus.ConsensusSignal
}

func (f *fakePub) PublishConsensus(s bus.ConsensusSignal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sigs = append(f.sigs, s)
	return nil
}

func (f *fakePub) snapshot() []bus.ConsensusSignal {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]bus.ConsensusSignal, len(f.sigs))
	copy(out, f.sigs)
	return out
}

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

func newAgg(t *testing.T, min int, win time.Duration, now func() time.Time) (*Aggregator, *fakePub) {
	t.Helper()
	st, err := window.Open(context.Background(), filepath.Join(t.TempDir(), "c.db"), win, min, now)
	if err != nil {
		t.Fatalf("window.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	fp := &fakePub{}
	return New(st, fp, win, now, nil), fp
}

func trade(wallet, token, side, size, price string) bus.TradeDetected {
	return bus.TradeDetected{Wallet: wallet, TokenID: token, Side: side, Size: size, Price: price}
}

func TestAggregator_FiresOnceThenGrows(t *testing.T) {
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	a, fp := newAgg(t, 3, 6*time.Hour, clk.now)
	ctx := context.Background()
	const tok, side = "TOK", "buy"

	a.Handle(ctx, trade("0xA", tok, side, "10", "0.5"))
	a.Handle(ctx, trade("0xB", tok, side, "10", "0.5"))
	if got := fp.snapshot(); len(got) != 0 {
		t.Fatalf("below threshold should not fire, got %d", len(got))
	}

	// 3rd distinct wallet => first fire.
	a.Handle(ctx, trade("0xC", tok, side, "10", "0.5"))
	if got := fp.snapshot(); len(got) != 1 {
		t.Fatalf("at 3 wallets want 1 signal, got %d", len(got))
	}

	// Same wallet repeats, and a re-fire at cohort 3, must NOT emit again.
	a.Handle(ctx, trade("0xA", tok, side, "10", "0.5"))
	a.Handle(ctx, trade("0xa", tok, side, "10", "0.5"))
	if got := fp.snapshot(); len(got) != 1 {
		t.Fatalf("repeats at cohort 3 must not re-fire, got %d", len(got))
	}

	// 4th distinct wallet => cohort grew => fire again.
	a.Handle(ctx, trade("0xD", tok, side, "10", "0.5"))
	got := fp.snapshot()
	if len(got) != 2 {
		t.Fatalf("growth to 4 want 2 signals total, got %d", len(got))
	}
	last := got[1]
	if last.Count != 4 {
		t.Fatalf("last signal count=%d want 4", last.Count)
	}
	wantWallets := []string{"0xa", "0xb", "0xc", "0xd"}
	if len(last.Wallets) != 4 {
		t.Fatalf("wallets=%v want 4 distinct", last.Wallets)
	}
	for i, w := range wantWallets {
		if last.Wallets[i] != w {
			t.Fatalf("wallets[%d]=%q want %q (sorted, lowercased)", i, last.Wallets[i], w)
		}
	}
	if last.WindowSecs != int(6*time.Hour/time.Second) {
		t.Fatalf("WindowSecs=%d want %d", last.WindowSecs, int(6*time.Hour/time.Second))
	}
}

func TestAggregator_SidesSeparate(t *testing.T) {
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	a, fp := newAgg(t, 2, 6*time.Hour, clk.now)
	ctx := context.Background()
	const tok = "TOK"

	a.Handle(ctx, trade("0xA", tok, "buy", "1", "1"))
	a.Handle(ctx, trade("0xB", tok, "buy", "1", "1"))  // buy reaches 2 -> fire
	a.Handle(ctx, trade("0xA", tok, "sell", "1", "1")) // sell only 1, no fire

	got := fp.snapshot()
	if len(got) != 1 {
		t.Fatalf("want 1 signal (buy only), got %d", len(got))
	}
	if got[0].Side != "buy" {
		t.Fatalf("side=%q want buy", got[0].Side)
	}
}

func TestAggregator_PruneStopsStaleConsensus(t *testing.T) {
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	a, fp := newAgg(t, 3, 6*time.Hour, clk.now)
	ctx := context.Background()
	const tok, side = "TOK", "buy"

	a.Handle(ctx, trade("0xA", tok, side, "1", "1"))
	a.Handle(ctx, trade("0xB", tok, side, "1", "1"))

	clk.advance(7 * time.Hour) // A and B fall out of window.

	a.Handle(ctx, trade("0xC", tok, side, "1", "1")) // only C in window -> 1
	a.Handle(ctx, trade("0xD", tok, side, "1", "1")) // C,D -> 2
	if got := fp.snapshot(); len(got) != 0 {
		t.Fatalf("stale A,B pruned, cohort < 3, want 0 signals, got %d", len(got))
	}
}

func TestAggregator_CombinedUSD(t *testing.T) {
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	a, fp := newAgg(t, 3, 6*time.Hour, clk.now)
	ctx := context.Background()
	const tok, side = "TOK", "buy"

	a.Handle(ctx, trade("0xA", tok, side, "10", "0.5"))  // 5
	a.Handle(ctx, trade("0xB", tok, side, "20", "0.25")) // 5
	a.Handle(ctx, trade("0xC", tok, side, "4", "1"))     // 4

	got := fp.snapshot()
	if len(got) != 1 {
		t.Fatalf("want 1 signal, got %d", len(got))
	}
	want := 14.0
	if got[0].CombinedUSD < want-1e-9 || got[0].CombinedUSD > want+1e-9 {
		t.Fatalf("CombinedUSD=%v want %v", got[0].CombinedUSD, want)
	}
}
