package main

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/pkg/svc"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/approval"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/config"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/handler"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/telegram"
)

const longPollSec = 5

func main() {
	svc.Run("notifier", run)
}

func run(ctx context.Context, log *slog.Logger) error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	log.Info("config loaded", "nats", cfg.NATSURL, "enrichment", cfg.EnrichmentAddr, "chat", cfg.TelegramChatID)

	conn, err := grpc.NewClient(cfg.EnrichmentAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	enrich := lobsterrollv1.NewEnrichmentClient(conn)

	// Give the HTTP client a timeout comfortably above the long-poll duration so
	// getUpdates is never cut short (and can't deadlock if longPollSec is raised).
	hc := &http.Client{Timeout: time.Duration(longPollSec)*time.Second + 20*time.Second}
	tg := telegram.New(telegram.DefaultBaseURL, cfg.TelegramToken, hc)
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

	alerts := handler.New(enrich, tg, cfg.TelegramChatID, log)
	mgr := approval.New(tg, pub, cfg.TelegramChatID, log)

	// One-way alerts on every detected trade.
	if _, err := sub.OnTradeDetected(cfg.QueueGroup, func(td bus.TradeDetected) {
		mctx, cancel := detached(ctx)
		defer cancel()
		alerts.Handle(mctx, td)
	}); err != nil {
		return err
	}
	// Two-way: post each proposal with approve/reject buttons.
	if _, err := sub.OnOrderProposed(cfg.QueueGroup, func(p bus.OrderProposal) {
		mctx, cancel := detached(ctx)
		defer cancel()
		mgr.OnProposal(mctx, p)
	}); err != nil {
		return err
	}

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return pollUpdates(ctx, tg, mgr, log) })
	log.Info("notifier listening (alerts + approval gate)")
	return g.Wait()
}

// pollUpdates long-polls Telegram and dispatches button taps and commands.
func pollUpdates(ctx context.Context, tg *telegram.Client, mgr *approval.Manager, log *slog.Logger) error {
	offset := 0
	for {
		if ctx.Err() != nil {
			return nil
		}
		ups, err := tg.GetUpdates(ctx, offset, longPollSec)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Warn("getUpdates failed", "err", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(2 * time.Second):
			}
			continue
		}
		for _, u := range ups {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			// Dispatch concurrently so a rate-limited callback (which may sleep on a
			// 429) can't block the poll loop or a /halt command behind it.
			switch {
			case u.CallbackQuery != nil:
				cb := *u.CallbackQuery
				go func() {
					dctx, cancel := detached(ctx)
					defer cancel()
					mgr.HandleCallback(dctx, cb)
				}()
			case u.Message != nil && strings.HasPrefix(u.Message.Text, "/"):
				msg := u.Message
				go func() {
					dctx, cancel := detached(ctx)
					defer cancel()
					mgr.HandleCommand(dctx, msg.Text, msg.Chat.ID, msg.From.Username)
				}()
			}
		}
	}
}

// detached returns a bounded context divorced from shutdown cancellation so an
// in-flight Telegram round-trip completes during the subscriber's drain.
func detached(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
}
