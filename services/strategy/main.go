package main

import (
	"context"
	"log/slog"

	"github.com/JacobTDang/LobsterRoll/pkg/svc"
)

func main() {
	svc.Run("strategy", func(ctx context.Context, log *slog.Logger) error {
		// TODO(phase5): consume trades.detected, apply sizing + slippage guard +
		// market/risk filters, publish orders.proposed.
		log.Info("idle scaffold; implement in phase 5")
		<-ctx.Done()
		return nil
	})
}
