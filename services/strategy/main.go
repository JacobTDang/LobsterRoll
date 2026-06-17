package main

import (
	"context"
	"log/slog"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/pkg/svc"
	"github.com/JacobTDang/LobsterRoll/services/strategy/internal/config"
	"github.com/JacobTDang/LobsterRoll/services/strategy/internal/handler"
	"github.com/JacobTDang/LobsterRoll/services/strategy/internal/marketdata"
)

func main() {
	svc.Run("strategy", run)
}

func run(ctx context.Context, log *slog.Logger) error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	log.Info("config loaded",
		"sizing", cfg.Policy.Sizing, "maxSlippage", cfg.Policy.MaxSlippage,
		"minLiquidity", cfg.Policy.MinLiquidityUSD, "allowlist", len(cfg.Allowlist),
		"nats", cfg.NATSURL, "gamma", cfg.GammaBase)

	pub, err := bus.Connect(cfg.NATSURL)
	if err != nil {
		return err
	}
	defer pub.Close()
	sub, err := bus.NewSubscriber(cfg.NATSURL)
	if err != nil {
		return err
	}
	defer sub.Close()

	src := marketdata.New(cfg.GammaBase, nil)
	h := handler.New(src, pub, cfg.Policy, cfg.Allowlist, log)

	if _, err := sub.OnTradeDetected(cfg.QueueGroup, func(td bus.TradeDetected) {
		h.Handle(ctx, td)
	}); err != nil {
		return err
	}

	log.Info("strategy listening for trades.detected")
	<-ctx.Done()
	return nil
}
