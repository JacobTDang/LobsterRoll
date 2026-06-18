package main

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/pkg/svc"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/config"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/handler"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/telegram"
)

func main() {
	svc.Run("notifier", run)
}

func run(ctx context.Context, log *slog.Logger) error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	log.Info("config loaded", "nats", cfg.NATSURL, "enrichment", cfg.EnrichmentAddr, "chat", cfg.TelegramChatID)

	// Enrichment gRPC client.
	conn, err := grpc.NewClient(cfg.EnrichmentAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	enrich := lobsterrollv1.NewEnrichmentClient(conn)

	// Telegram + NATS.
	tg := telegram.New(telegram.DefaultBaseURL, cfg.TelegramToken, nil)
	sub, err := bus.NewSubscriber(cfg.NATSURL, log)
	if err != nil {
		return err
	}
	defer sub.Close()

	h := handler.New(enrich, tg, cfg.TelegramChatID, log)
	if _, err := sub.OnTradeDetected(cfg.QueueGroup, func(td bus.TradeDetected) {
		// Detach from the shutdown-cancelled ctx (but keep a bound) so an alert
		// in flight when SIGTERM arrives still gets enriched and delivered during
		// the subscriber's drain.
		mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		h.Handle(mctx, td)
	}); err != nil {
		return err
	}

	log.Info("notifier listening for trades.detected")
	<-ctx.Done()
	return nil
}
