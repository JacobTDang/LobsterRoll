package main

import (
	"context"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/pkg/svc"
	"github.com/JacobTDang/LobsterRoll/services/pricewatch/internal/capture"
	"github.com/JacobTDang/LobsterRoll/services/pricewatch/internal/client"
	"github.com/JacobTDang/LobsterRoll/services/pricewatch/internal/config"
	"github.com/JacobTDang/LobsterRoll/services/pricewatch/internal/store"
)

func main() {
	svc.Run("pricewatch", run)
}

func run(ctx context.Context, log *slog.Logger) error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	log.Info("config loaded", "nats", cfg.NATSURL, "clob", cfg.CLOBBase, "db", cfg.DBPath,
		"poll", cfg.PollInterval, "tokenTTL", cfg.TokenTTL)

	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	tracker := capture.New(client.New(cfg.CLOBBase, nil), st, cfg.TokenTTL, log)

	sub, err := bus.NewSubscriber(cfg.NATSURL, log)
	if err != nil {
		return err
	}
	defer sub.Close()

	// Every detected trade marks its token as active to snapshot.
	if _, err := sub.OnTradeDetected(cfg.QueueGroup, func(td bus.TradeDetected) {
		tracker.Track(td.TokenID, time.Now().Unix())
	}); err != nil {
		return err
	}

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return pollLoop(ctx, tracker, st, cfg, log) })
	log.Info("pricewatch capturing", "interval", cfg.PollInterval)
	return g.Wait()
}

// pollLoop snapshots every active token each PollInterval and prunes old rows.
func pollLoop(ctx context.Context, tracker *capture.Tracker, st *store.Store, cfg config.Config, log *slog.Logger) error {
	t := time.NewTicker(cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			now := time.Now().Unix()
			tracker.Poll(ctx, now)
			if err := st.Prune(ctx, now-int64(cfg.Retention.Seconds())); err != nil {
				log.Warn("snapshot prune failed", "err", err)
			}
		}
	}
}
