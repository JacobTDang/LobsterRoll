package main

import (
	"context"
	"log/slog"

	"github.com/JacobTDang/LobsterRoll/pkg/svc"
)

func main() {
	svc.Run("watcher", func(ctx context.Context, log *slog.Logger) error {
		// TODO(phase2): subscribe to OrderFilled on CTF Exchange V1+V2 over WSS,
		// decode logs, filter by watchset, persist last_processed_block, backfill
		// gaps on reconnect, dedup by (txHash, logIndex), publish trades.detected.
		log.Info("idle scaffold; implement in phase 2")
		<-ctx.Done()
		return nil
	})
}
