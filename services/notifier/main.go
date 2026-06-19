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
	"github.com/JacobTDang/LobsterRoll/pkg/dedup"
	"github.com/JacobTDang/LobsterRoll/pkg/metrics"
	"github.com/JacobTDang/LobsterRoll/pkg/svc"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/approval"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/config"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/handler"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/positions"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/telegram"
)

const longPollSec = 5

// Alert dispatch pool sizing: workers cap concurrent Telegram sends (Telegram
// rate-limits ~30/s, so a few is plenty); the queue absorbs bursts before drops.
const (
	alertWorkers   = 4
	alertQueueSize = 2048
)

var mAlertsDropped = metrics.NewCounter("lobsterroll_notifier_alerts_dropped_total", "alerts dropped because the dispatch queue was full (telegram saturated)")

func main() {
	svc.Run("notifier", run)
}

func run(ctx context.Context, log *slog.Logger) error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	log.Info("config loaded", "nats", cfg.NATSURL, "enrichment", cfg.EnrichmentAddr, "leaderboard", cfg.LeaderboardAddr, "chat", cfg.TelegramChatID)

	conn, err := grpc.NewClient(cfg.EnrichmentAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	enrich := lobsterrollv1.NewEnrichmentClient(conn)

	lbConn, err := grpc.NewClient(cfg.LeaderboardAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer lbConn.Close()
	leaderboard := lobsterrollv1.NewLeaderboardClient(lbConn)

	// Give the HTTP client a timeout comfortably above the long-poll duration so
	// getUpdates is never cut short (and can't deadlock if longPollSec is raised).
	hc := &http.Client{Timeout: time.Duration(longPollSec)*time.Second + 20*time.Second}
	tg := telegram.New(cfg.TelegramBaseURL, cfg.TelegramToken, hc) // "" -> DefaultBaseURL
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

	var cooldown *dedup.TTLSet
	if cfg.AlertCooldown > 0 {
		cooldown = dedup.New(cfg.AlertCooldown)
	}

	// Position-exit alerts: enabled only when a public USER_WALLET is configured.
	var myPos *positions.Cache
	var posClient *positions.Client
	if cfg.UserWallet != "" {
		myPos = positions.NewCache(cfg.UserWallet)
		posClient = positions.New(cfg.DataAPIBase, nil)
		log.Info("position-exit alerts enabled", "wallet", cfg.UserWallet, "poll", cfg.MyPositionsPoll)
	}

	alerts := handler.New(enrich, leaderboard, tg, cfg.TelegramChatID,
		dedup.New(cfg.AlertDedupTTL), cooldown, myPos, dedup.New(cfg.ConsensusDedup), log)
	mgr := approval.New(tg, pub, cfg.TelegramChatID, log)

	g, ctx := errgroup.WithContext(ctx)

	// Alert dispatch pool: a Telegram Send can block for tens of seconds (timeout +
	// 429 backoff). The core-NATS subscription callback runs serially on one
	// goroutine, so calling Send inline lets a slow Telegram stall the
	// trades.detected drain and trip NATS pending limits → silently dropped
	// messages. Hand sends to a bounded worker pool instead; if the queue saturates
	// we drop EXPLICITLY (metered) rather than letting NATS drop silently.
	jobs := make(chan func(), alertQueueSize)
	for i := 0; i < alertWorkers; i++ {
		g.Go(func() error {
			for {
				select {
				case <-ctx.Done():
					return nil
				case f := <-jobs:
					f()
				}
			}
		})
	}
	submit := func(f func()) {
		select {
		case jobs <- f:
		default:
			mAlertsDropped.Inc()
			log.Warn("alert dispatch queue full; dropping alert (telegram slow/rate-limited?)")
		}
	}

	// One-way alerts on every detected trade (dispatched off the NATS callback).
	if _, err := sub.OnTradeDetected(cfg.QueueGroup, func(td bus.TradeDetected) {
		submit(func() {
			mctx, cancel := svc.Detached(ctx)
			defer cancel()
			alerts.Handle(mctx, td)
		})
	}); err != nil {
		return err
	}
	// Premium consensus alerts when multiple tracked wallets converge on a bet.
	if _, err := sub.OnConsensus(cfg.QueueGroup, func(sig bus.ConsensusSignal) {
		submit(func() {
			mctx, cancel := svc.Detached(ctx)
			defer cancel()
			alerts.HandleConsensus(mctx, sig)
		})
	}); err != nil {
		return err
	}
	// Two-way: post each proposal with approve/reject buttons. Low-volume +
	// interactive, so kept on the callback goroutine (not the bursty alert path).
	if _, err := sub.OnOrderProposed(cfg.QueueGroup, func(p bus.OrderProposal) {
		mctx, cancel := svc.Detached(ctx)
		defer cancel()
		mgr.OnProposal(mctx, p)
	}); err != nil {
		return err
	}

	g.Go(func() error { return pollUpdates(ctx, tg, mgr, log) })
	if myPos != nil {
		g.Go(func() error { return pollPositions(ctx, posClient, myPos, cfg.UserWallet, cfg.MyPositionsPoll, log) })
	}
	log.Info("notifier listening (alerts + approval gate)")
	return g.Wait()
}

// pollPositions refreshes the user's open positions on an interval (and once at
// startup). A fetch error keeps the last good snapshot rather than going dark.
func pollPositions(ctx context.Context, c *positions.Client, cache *positions.Cache, wallet string, every time.Duration, log *slog.Logger) error {
	refresh := func() {
		fctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		ps, err := c.Fetch(fctx, wallet)
		if err != nil {
			log.Warn("positions refresh failed; keeping last snapshot", "err", err)
			return
		}
		cache.Replace(ps)
		log.Info("positions refreshed", "count", len(ps))
	}
	refresh()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			refresh()
		}
	}
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
					dctx, cancel := svc.Detached(ctx)
					defer cancel()
					mgr.HandleCallback(dctx, cb)
				}()
			case u.Message != nil && strings.HasPrefix(u.Message.Text, "/"):
				msg := u.Message
				go func() {
					dctx, cancel := svc.Detached(ctx)
					defer cancel()
					mgr.HandleCommand(dctx, msg.Text, msg.Chat.ID, msg.From.Username)
				}()
			}
		}
	}
}
