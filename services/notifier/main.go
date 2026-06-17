package main

import (
	"context"
	"log/slog"

	"github.com/JacobTDang/LobsterRoll/pkg/svc"
)

func main() {
	svc.Run("notifier", func(ctx context.Context, log *slog.Logger) error {
		// TODO(phase4/6): Telegram bot. Phase 4: one-way alerts from trades.detected.
		// Phase 6: two-way — inline approve/reject buttons -> orders.approved/rejected,
		// and a /halt command -> control.halt.
		log.Info("idle scaffold; implement in phase 4")
		<-ctx.Done()
		return nil
	})
}
