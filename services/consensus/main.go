package main

import (
	"context"
	"log/slog"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/pkg/svc"
	"github.com/JacobTDang/LobsterRoll/services/consensus/internal/aggregator"
	"github.com/JacobTDang/LobsterRoll/services/consensus/internal/config"
	"github.com/JacobTDang/LobsterRoll/services/consensus/internal/window"
)

func main() {
	svc.Run("consensus", run)
}

func run(ctx context.Context, log *slog.Logger) error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	log.Info("config loaded",
		"nats", cfg.NATSURL, "minWallets", cfg.MinWallets,
		"window", cfg.Window, "db", cfg.DBPath, "queue", cfg.QueueGroup)

	store, err := window.Open(ctx, cfg.DBPath, cfg.Window, cfg.MinWallets, nil)
	if err != nil {
		return err
	}
	defer store.Close()

	pub, err := bus.Connect(cfg.NATSURL)
	if err != nil {
		return err
	}
	defer pub.Close()

	sub, err := bus.NewSubscriber(cfg.NATSURL, log)
	if err != nil {
		return err
	}
	defer sub.Close()

	agg := aggregator.New(store, pub, cfg.Window, nil, log)

	if _, err := sub.OnTradeDetected(cfg.QueueGroup, func(td bus.TradeDetected) {
		// Detach from the shutdown-cancelled ctx so an in-flight aggregation at
		// SIGTERM still completes during drain.
		agg.Handle(context.WithoutCancel(ctx), td)
	}); err != nil {
		return err
	}

	log.Info("consensus listening for trades.detected")
	<-ctx.Done()
	return nil
}
