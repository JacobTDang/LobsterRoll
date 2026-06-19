package sizer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"google.golang.org/grpc"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/pkg/sizing"
	"github.com/JacobTDang/LobsterRoll/services/strategy/internal/book"
)

type fakeStats struct {
	resp *lobsterrollv1.WalletStats
	err  error
}

func (f fakeStats) GetWalletStats(context.Context, *lobsterrollv1.GetWalletStatsRequest, ...grpc.CallOption) (*lobsterrollv1.WalletStats, error) {
	return f.resp, f.err
}

type fakeBooks struct {
	b   book.Book
	err error
}

func (f fakeBooks) Book(context.Context, string) (book.Book, error) { return f.b, f.err }

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func cfg() sizing.Config {
	return sizing.Config{
		KellyFraction: 0.25, EdgeBuffer: 0.02, MaxSpread: 0.04,
		PerBetFrac: 0.03, MaxExposureFrac: 0.10, DDDerisk: 0.10, DDStop: 0.20,
		CLVFull: 50, MinStakeUSD: 1,
	}
}

// a tradable book: mid 0.50, half-spread 0.01, deep.
func goodBook() book.Book {
	return book.Book{
		Bids: []book.Level{{Price: 0.49, Size: 100000}},
		Asks: []book.Level{{Price: 0.51, Size: 100000}},
	}
}

func goodStats() *lobsterrollv1.WalletStats {
	return &lobsterrollv1.WalletStats{Found: true, Fresh: true, AvgClv: 0.10, ClvN: 100, Roi: 0.3, PortfolioValue: 500000}
}

func newSizer(s StatsLookuper, b BookSource) *Sizer {
	return New(s, b, cfg(), 10_000, 0.02, quiet())
}

func TestSize_Sizes(t *testing.T) {
	d := newSizer(fakeStats{resp: goodStats()}, fakeBooks{b: goodBook()}).Size(context.Background(), bus.TradeDetected{Wallet: "w", TokenID: "t", Side: "buy"})
	if d.Reason != "" {
		t.Fatalf("unexpected skip: %q", d.Reason)
	}
	if d.Stake <= 0 {
		t.Errorf("stake = %v, want > 0", d.Stake)
	}
}

func TestSize_SkipPaths(t *testing.T) {
	td := bus.TradeDetected{Wallet: "w", TokenID: "t", Side: "buy"}
	cases := []struct {
		name string
		s    StatsLookuper
		b    BookSource
		want string
	}{
		{"stats error", fakeStats{err: errors.New("down")}, fakeBooks{b: goodBook()}, "stats lookup failed: down"},
		{"no stats", fakeStats{resp: &lobsterrollv1.WalletStats{Found: false}}, fakeBooks{b: goodBook()}, "no leader stats"},
		{"book error", fakeStats{resp: goodStats()}, fakeBooks{err: errors.New("clob down")}, "book fetch failed: clob down"},
		{"empty book", fakeStats{resp: goodStats()}, fakeBooks{b: book.Book{}}, "no book mid"},
	}
	for _, c := range cases {
		if d := newSizer(c.s, c.b).Size(context.Background(), td); d.Reason != c.want {
			t.Errorf("%s: reason = %q, want %q", c.name, d.Reason, c.want)
		}
	}
}

func TestSize_EngineVetoPropagates(t *testing.T) {
	// A cooling leader -> the engine vetoes -> sizer returns that reason.
	st := goodStats()
	st.Fresh = false
	d := newSizer(fakeStats{resp: st}, fakeBooks{b: goodBook()}).Size(context.Background(), bus.TradeDetected{Wallet: "w", TokenID: "t", Side: "buy"})
	if d.Reason != "leader cooling off" {
		t.Errorf("reason = %q, want 'leader cooling off'", d.Reason)
	}
}
