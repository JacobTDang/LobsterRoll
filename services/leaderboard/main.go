package main

import (
	"context"
	"log/slog"

	"github.com/JacobTDang/LobsterRoll/pkg/svc"
)

func main() {
	svc.Run("leaderboard", func(ctx context.Context, log *slog.Logger) error {
		// TODO(phase1): sync top-N from Polymarket data-api leaderboard into the
		// watchset (SQLite) on a ticker; serve GetWatchset/StreamWatchset over gRPC.
		log.Info("idle scaffold; implement in phase 1")
		<-ctx.Done()
		return nil
	})
}
