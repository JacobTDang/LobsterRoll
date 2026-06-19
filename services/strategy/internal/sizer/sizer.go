// Package sizer orchestrates position sizing for the strategy: it gathers the
// live inputs (leader track record from the leaderboard, order book from the
// CLOB) and runs the pure pkg/sizing engine. A nil *Sizer means sizing is
// disabled and the strategy falls back to its policy size.
package sizer

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/pkg/sizing"
	"github.com/JacobTDang/LobsterRoll/services/strategy/internal/book"
)

// StatsLookuper resolves a leader's track record. The generated
// lobsterrollv1.LeaderboardClient satisfies it.
type StatsLookuper interface {
	GetWalletStats(ctx context.Context, in *lobsterrollv1.GetWalletStatsRequest, opts ...grpc.CallOption) (*lobsterrollv1.WalletStats, error)
}

// BookSource fetches an order book. *book.Client satisfies it.
type BookSource interface {
	Book(ctx context.Context, tokenID string) (book.Book, error)
}

// Sizer computes a stake for a copy signal.
type Sizer struct {
	stats    StatsLookuper
	books    BookSource
	cfg      sizing.Config
	bankroll float64
	slipBand float64 // depth band for BuyDepthUSD (price units)
	log      *slog.Logger
}

// New constructs a Sizer.
func New(stats StatsLookuper, books BookSource, cfg sizing.Config, bankroll, slipBand float64, log *slog.Logger) *Sizer {
	return &Sizer{stats: stats, books: books, cfg: cfg, bankroll: bankroll, slipBand: slipBand, log: log}
}

// Size gathers inputs and runs the engine. A skip (Decision.Stake==0 with a
// Reason) covers both engine refusals and missing inputs, so callers handle one
// path. Only buy signals are sized (we copy entries).
func (s *Sizer) Size(ctx context.Context, td bus.TradeDetected) sizing.Decision {
	st, err := s.stats.GetWalletStats(ctx, &lobsterrollv1.GetWalletStatsRequest{Wallet: td.Wallet})
	if err != nil {
		return sizing.Decision{Reason: "stats lookup failed: " + err.Error()}
	}
	if !st.GetFound() {
		return sizing.Decision{Reason: "no leader stats"}
	}

	b, err := s.books.Book(ctx, td.TokenID)
	if err != nil {
		return sizing.Decision{Reason: "book fetch failed: " + err.Error()}
	}
	mid, ok := b.Mid()
	if !ok {
		return sizing.Decision{Reason: "no book mid"}
	}
	halfSpread, _ := b.HalfSpread()

	return sizing.Size(sizing.Inputs{
		Price:       mid,
		HalfSpread:  halfSpread,
		AvgCLV:      st.GetAvgClv(),
		CLVN:        int(st.GetClvN()),
		ShrunkROI:   st.GetRoi(), // served ROI (already from strictly-gated wallets)
		Fresh:       st.GetFresh(),
		Bankroll:    s.bankroll,
		Exposure:    0, // strategy doesn't track exposure; the trader enforces the hard cap
		DepthCapUSD: b.BuyDepthUSD(s.slipBand),
	}, s.cfg)
}
