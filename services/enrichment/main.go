package main

import (
	"context"
	"log/slog"

	"github.com/JacobTDang/LobsterRoll/pkg/svc"
)

func main() {
	svc.Run("enrichment", func(ctx context.Context, log *slog.Logger) error {
		// TODO(phase3): resolve token_id -> market/outcome via gamma/clob, cache in
		// SQLite, serve EnrichToken over gRPC.
		log.Info("idle scaffold; implement in phase 3")
		<-ctx.Done()
		return nil
	})
}
