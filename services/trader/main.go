package main

import (
	"context"
	"log/slog"

	"github.com/JacobTDang/LobsterRoll/pkg/svc"
)

func main() {
	svc.Run("trader", func(ctx context.Context, log *slog.Logger) error {
		// TODO(phase7): consume orders.approved, sign orders via go-order-utils
		// (EIP-712), place on the CLOB. SOLE holder of the private key. Enforce
		// hard caps (per-trade/day/exposure) independent of strategy. Honor
		// control.halt. Publish orders.filled / orders.failed.
		log.Info("idle scaffold; implement in phase 7")
		<-ctx.Done()
		return nil
	})
}
