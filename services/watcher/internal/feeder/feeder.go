// Package feeder keeps the watcher's watchset in sync with leaderboard-svc via
// gRPC: an initial GetWatchset snapshot followed by a StreamWatchset diff feed,
// reconnecting with backoff.
package feeder

import (
	"context"
	"log/slog"
	"time"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/backoff"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/watchset"
)

// Feeder applies leaderboard updates to a watchset.Set.
type Feeder struct {
	client lobsterrollv1.LeaderboardClient
	set    *watchset.Set
	log    *slog.Logger
}

// New returns a Feeder.
func New(client lobsterrollv1.LeaderboardClient, set *watchset.Set, log *slog.Logger) *Feeder {
	return &Feeder{client: client, set: set, log: log}
}

// Snapshot fetches the full watchset once and applies it.
func (f *Feeder) Snapshot(ctx context.Context) error {
	resp, err := f.client.GetWatchset(ctx, &lobsterrollv1.GetWatchsetRequest{})
	if err != nil {
		return err
	}
	f.set.Apply(resp.GetWallets(), nil)
	return nil
}

// Stream consumes StreamWatchset diffs until the stream errors or ctx is done.
func (f *Feeder) Stream(ctx context.Context) error {
	stream, err := f.client.StreamWatchset(ctx, &lobsterrollv1.StreamWatchsetRequest{})
	if err != nil {
		return err
	}
	for {
		upd, err := stream.Recv()
		if err != nil {
			return err
		}
		f.set.Apply(upd.GetAdded(), upd.GetRemoved())
	}
}

// Run maintains the watchset for the life of ctx: snapshot, then stream, then
// on any error wait (capped exponential backoff) and resync. Returns nil when
// ctx is cancelled.
//
// ready, if non-nil, is closed once the first watchset snapshot has been applied
// so a consumer (the watcher) can avoid backfilling against an empty watchset.
func (f *Feeder) Run(ctx context.Context, ready chan<- struct{}) error {
	const (
		base = 1 * time.Second
		max  = 30 * time.Second
	)
	readyClosed := false
	attempt := 0
	for {
		if err := f.Snapshot(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			f.log.Warn("watchset snapshot failed", "err", err)
		} else {
			if ready != nil && !readyClosed {
				close(ready)
				readyClosed = true
			}
			attempt = 0 // a good snapshot resets backoff
			if err := f.Stream(ctx); err != nil && ctx.Err() == nil {
				f.log.Warn("watchset stream ended", "err", err)
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff.Delay(attempt, base, max)):
		}
		attempt++
	}
}
