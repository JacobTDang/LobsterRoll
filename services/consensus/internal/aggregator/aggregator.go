// Package aggregator turns detected trades into consensus signals: it records
// each trade in the rolling window and publishes a bus.ConsensusSignal when a
// fresh cohort of distinct tracked wallets reaches the configured threshold.
package aggregator

import (
	"context"
	"log/slog"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/pkg/metrics"
	"github.com/JacobTDang/LobsterRoll/services/consensus/internal/window"
)

var mFired = metrics.NewCounter("lobsterroll_consensus_fired_total", "consensus signals published")

// Publisher publishes consensus signals.
type Publisher interface {
	PublishConsensus(bus.ConsensusSignal) error
}

// Recorder is the window store the aggregator depends on. Record ingests a trade
// and returns the current cohort plus whether a consensus signal should fire.
type Recorder interface {
	Record(ctx context.Context, ev bus.TradeDetected) (window.Cohort, bool, error)
}

// Aggregator processes detected trades and emits consensus signals.
type Aggregator struct {
	store      Recorder
	pub        Publisher
	windowSecs int
	now        func() time.Time
	log        *slog.Logger
}

// New constructs an Aggregator. now defaults to time.Now if nil; log defaults to
// slog.Default if nil. The fire threshold lives in the window store.
func New(store Recorder, pub Publisher, win time.Duration, now func() time.Time, log *slog.Logger) *Aggregator {
	if now == nil {
		now = time.Now
	}
	if log == nil {
		log = slog.Default()
	}
	return &Aggregator{
		store:      store,
		pub:        pub,
		windowSecs: int(win / time.Second),
		now:        now,
		log:        log,
	}
}

// Handle records the trade and, when the window store reports a fresh cohort has
// reached the threshold (or grown to a new max), publishes a consensus signal.
func (a *Aggregator) Handle(ctx context.Context, td bus.TradeDetected) {
	cohort, fire, err := a.store.Record(ctx, td)
	if err != nil {
		a.log.Error("record trade", "err", err, "token", td.TokenID, "side", td.Side)
		return
	}
	if !fire {
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
	mFired.Inc()
	a.log.Info("consensus signal",
		"token", sig.TokenID, "side", sig.Side, "count", sig.Count,
		"combined_usd", sig.CombinedUSD, "window_secs", sig.WindowSecs)
}
