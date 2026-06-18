// Package aggregator turns detected trades into consensus signals: it records
// each trade in the rolling window and publishes a bus.ConsensusSignal when a
// fresh cohort of distinct tracked wallets reaches the configured threshold.
package aggregator

import (
	"context"
	"log/slog"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/services/consensus/internal/window"
)

// Publisher publishes consensus signals.
type Publisher interface {
	PublishConsensus(bus.ConsensusSignal) error
}

// Recorder is the window store the aggregator depends on.
type Recorder interface {
	Record(ctx context.Context, ev bus.TradeDetected) (window.Cohort, error)
	ShouldFire(ctx context.Context, tokenID, side string, count int) (bool, error)
}

// Aggregator processes detected trades and emits consensus signals.
type Aggregator struct {
	store      Recorder
	pub        Publisher
	minWallets int
	windowSecs int
	now        func() time.Time
	log        *slog.Logger
}

// New constructs an Aggregator. now defaults to time.Now if nil; log defaults to
// slog.Default if nil.
func New(store Recorder, pub Publisher, minWallets int, win time.Duration, now func() time.Time, log *slog.Logger) *Aggregator {
	if now == nil {
		now = time.Now
	}
	if log == nil {
		log = slog.Default()
	}
	return &Aggregator{
		store:      store,
		pub:        pub,
		minWallets: minWallets,
		windowSecs: int(win / time.Second),
		now:        now,
		log:        log,
	}
}

// Handle records the trade and, if a fresh cohort of >= minWallets distinct
// wallets is reached for the trade's (token, side), publishes a consensus signal.
func (a *Aggregator) Handle(ctx context.Context, td bus.TradeDetected) {
	cohort, err := a.store.Record(ctx, td)
	if err != nil {
		a.log.Error("record trade", "err", err, "token", td.TokenID, "side", td.Side)
		return
	}
	if cohort.Count() < a.minWallets {
		return
	}
	fired, err := a.store.ShouldFire(ctx, td.TokenID, td.Side, cohort.Count())
	if err != nil {
		a.log.Error("should-fire check", "err", err, "token", td.TokenID, "side", td.Side)
		return
	}
	if !fired {
		return
	}
	sig := bus.ConsensusSignal{
		TokenID:     td.TokenID,
		Side:        td.Side,
		Wallets:     cohort.Wallets, // already distinct, lowercased, sorted
		Count:       cohort.Count(),
		CombinedUSD: cohort.CombinedUSD,
		WindowSecs:  a.windowSecs,
		ObservedAt:  a.now(),
	}
	if err := a.pub.PublishConsensus(sig); err != nil {
		a.log.Error("publish consensus", "err", err, "token", td.TokenID, "side", td.Side)
		return
	}
	a.log.Info("consensus signal",
		"token", sig.TokenID, "side", sig.Side, "count", sig.Count,
		"combined_usd", sig.CombinedUSD, "window_secs", sig.WindowSecs)
}
