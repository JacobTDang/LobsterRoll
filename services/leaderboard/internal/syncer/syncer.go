// Package syncer periodically refreshes the watchset from the leaderboard and
// broadcasts any change to gRPC stream subscribers.
package syncer

import (
	"context"
	"log/slog"
	"time"

	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/client"
	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/store"
)

// Fetcher returns the current top-N wallets for a metric/window.
type Fetcher interface {
	Fetch(ctx context.Context, metric client.Metric, window client.Window, topN int) ([]string, error)
}

// Storer atomically replaces the watchset, reports what changed, and records
// when the last successful sync completed.
type Storer interface {
	Replace(ctx context.Context, wallets []string) (store.Delta, error)
	SetLastSync(ctx context.Context, unix int64) error
}

// Broadcaster publishes a watchset change to subscribers.
type Broadcaster interface {
	Broadcast(added, removed []string)
}

// Syncer wires a Fetcher, Storer, and Broadcaster on a fixed interval.
type Syncer struct {
	fetch    Fetcher
	store    Storer
	bc       Broadcaster
	metric   client.Metric
	window   client.Window
	topN     int
	interval time.Duration
	log      *slog.Logger
}

// New constructs a Syncer.
func New(f Fetcher, s Storer, bc Broadcaster, metric client.Metric, window client.Window, topN int, interval time.Duration, log *slog.Logger) *Syncer {
	return &Syncer{
		fetch:    f,
		store:    s,
		bc:       bc,
		metric:   metric,
		window:   window,
		topN:     topN,
		interval: interval,
		log:      log,
	}
}

// Run performs an immediate sync, then re-syncs every interval until ctx is
// cancelled. Transient sync errors are logged, not fatal — the loop keeps going.
func (s *Syncer) Run(ctx context.Context) error {
	if err := s.syncOnce(ctx); err != nil {
		s.log.Warn("initial watchset sync failed", "err", err)
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.syncOnce(ctx); err != nil {
				s.log.Warn("watchset sync failed", "err", err)
			}
		}
	}
}

func (s *Syncer) syncOnce(ctx context.Context) error {
	wallets, err := s.fetch.Fetch(ctx, s.metric, s.window, s.topN)
	if err != nil {
		return err
	}
	// Empty-replace guard (defense-in-depth): never wipe the watchset from an
	// empty fetch. Skip the replace/broadcast and do not advance last-sync —
	// this is an unhealthy sync that should surface as staleness.
	if len(wallets) == 0 {
		s.log.Warn("fetched empty watchset; skipping replace to avoid wiping watchset")
		return nil
	}
	d, err := s.store.Replace(ctx, wallets)
	if err != nil {
		return err
	}
	if !d.Empty() {
		s.bc.Broadcast(d.Added, d.Removed)
		s.log.Info("watchset changed", "added", len(d.Added), "removed", len(d.Removed), "size", len(wallets))
	}
	// Record the last successful sync on every healthy fetch, including no-op
	// syncs where the watchset did not change, so staleness reflects the last
	// successful fetch rather than the last change.
	if err := s.store.SetLastSync(ctx, time.Now().Unix()); err != nil {
		return err
	}
	return nil
}
