// Package settle computes Closing Line Value for observed trades once their
// market has resolved: close = the snapshot near (endDate - buffer), then
// CLV(entry, close, side). Trades whose market hasn't resolved, has no known end
// date, or has no captured snapshot are left for a later pass (graceful sparsity).
package settle

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/metrics"
	"github.com/JacobTDang/LobsterRoll/services/pricewatch/internal/clv"
	"github.com/JacobTDang/LobsterRoll/services/pricewatch/internal/store"
)

var mSettled = metrics.NewCounter("lobsterroll_pricewatch_clv_settled_total", "trades with computed CLV")

// TradeStore is the subset of the snapshot store the settler needs.
type TradeStore interface {
	UnsettledTrades(ctx context.Context) ([]store.Trade, error)
	Nearest(ctx context.Context, tokenID string, targetTS int64) (store.Snapshot, error)
	SetTradeCLV(ctx context.Context, tx string, logIndex uint64, wallet string, clv float64) error
}

// EndDater resolves a token's market end (unix seconds); 0 = unknown.
type EndDater interface {
	EndDate(ctx context.Context, tokenID string) (int64, error)
}

// Settler computes CLV for resolved trades.
type Settler struct {
	store  TradeStore
	ends   EndDater
	buffer time.Duration // close = snapshot near (endDate - buffer)
	now    func() time.Time
	log    *slog.Logger
}

// New constructs a Settler. buffer picks a late-but-liquid pre-close snapshot.
func New(s TradeStore, ends EndDater, buffer time.Duration, log *slog.Logger) *Settler {
	return &Settler{store: s, ends: ends, buffer: buffer, now: time.Now, log: log}
}

// Run settles every currently-resolvable unsettled trade. Errors are logged and
// skipped so one bad trade can't stall the pass.
func (s *Settler) Run(ctx context.Context) {
	trades, err := s.store.UnsettledTrades(ctx)
	if err != nil {
		s.log.Warn("list unsettled trades failed", "err", err)
		return
	}
	now := s.now().Unix()
	buf := int64(s.buffer.Seconds())
	var settled int
	for _, t := range trades {
		end, err := s.ends.EndDate(ctx, t.TokenID)
		if err != nil {
			s.log.Warn("end-date lookup failed; will retry", "token", t.TokenID, "err", err)
			continue
		}
		if end == 0 || now < end {
			continue // unknown end, or market not resolved yet
		}
		snap, err := s.store.Nearest(ctx, t.TokenID, end-buf)
		if err != nil {
			if !errors.Is(err, store.ErrNoSnapshot) {
				s.log.Warn("close snapshot lookup failed", "token", t.TokenID, "err", err)
			}
			continue // no captured close -> leave unsettled (won't contribute)
		}
		v := clv.CLV(t.Entry, snap.Mid, t.Buy)
		if err := s.store.SetTradeCLV(ctx, t.Tx, t.LogIndex, t.Wallet, v); err != nil {
			s.log.Warn("set clv failed", "tx", t.Tx, "err", err)
			continue
		}
		settled++
	}
	if settled > 0 {
		mSettled.Add(float64(settled))
		s.log.Info("settled CLV", "count", settled)
	}
}
