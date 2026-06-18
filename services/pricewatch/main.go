package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/pkg/svc"
	"github.com/JacobTDang/LobsterRoll/services/pricewatch/internal/capture"
	"github.com/JacobTDang/LobsterRoll/services/pricewatch/internal/client"
	"github.com/JacobTDang/LobsterRoll/services/pricewatch/internal/config"
	"github.com/JacobTDang/LobsterRoll/services/pricewatch/internal/enrich"
	"github.com/JacobTDang/LobsterRoll/services/pricewatch/internal/server"
	"github.com/JacobTDang/LobsterRoll/services/pricewatch/internal/settle"
	"github.com/JacobTDang/LobsterRoll/services/pricewatch/internal/store"
)

func main() {
	svc.Run("pricewatch", run)
}

func run(ctx context.Context, log *slog.Logger) error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	log.Info("config loaded", "nats", cfg.NATSURL, "clob", cfg.CLOBBase, "db", cfg.DBPath,
		"poll", cfg.PollInterval, "tokenTTL", cfg.TokenTTL)

	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	tracker := capture.New(client.New(cfg.CLOBBase, nil), st, cfg.TokenTTL, log)

	conn, err := grpc.NewClient(cfg.EnrichmentAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	settler := settle.New(st, enrich.New(lobsterrollv1.NewEnrichmentClient(conn)), cfg.CloseBuffer, log)

	sub, err := bus.NewSubscriber(cfg.NATSURL, log)
	if err != nil {
		return err
	}
	defer sub.Close()

	// Every detected trade marks its token active to snapshot AND is recorded so
	// its CLV can be computed once the market resolves.
	if _, err := sub.OnTradeDetected(cfg.QueueGroup, func(td bus.TradeDetected) {
		now := time.Now().Unix()
		tracker.Track(td.TokenID, now)
		entry, perr := strconv.ParseFloat(td.Price, 64)
		if perr != nil {
			log.Warn("unparseable trade price; skipping CLV record", "tx", td.TxHash, "price", td.Price)
			return
		}
		if err := st.RecordTrade(ctx, store.Trade{
			Wallet: td.Wallet, TokenID: td.TokenID, Tx: td.TxHash, LogIndex: td.LogIndex,
			Entry: entry, Buy: strings.EqualFold(td.Side, "buy"), ObservedUnix: now,
		}); err != nil {
			log.Warn("record trade failed", "tx", td.TxHash, "err", err)
		}
	}); err != nil {
		return err
	}

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return err
	}
	gs := grpc.NewServer()
	lobsterrollv1.RegisterPricewatchServer(gs, server.New(st))

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return pollLoop(ctx, tracker, st, cfg, log) })
	g.Go(func() error { return settleLoop(ctx, settler, cfg, log) })
	g.Go(func() error {
		log.Info("grpc serving", "addr", cfg.GRPCAddr)
		if err := gs.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			return err
		}
		return nil
	})
	g.Go(func() error {
		<-ctx.Done()
		gs.GracefulStop()
		return nil
	})
	log.Info("pricewatch capturing", "interval", cfg.PollInterval, "settle", cfg.SettleInterval)
	return g.Wait()
}

// settleLoop computes CLV for resolved trades every SettleInterval.
func settleLoop(ctx context.Context, settler *settle.Settler, cfg config.Config, _ *slog.Logger) error {
	t := time.NewTicker(cfg.SettleInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			settler.Run(ctx)
		}
	}
}

// pollLoop snapshots every active token each PollInterval and prunes old rows.
func pollLoop(ctx context.Context, tracker *capture.Tracker, st *store.Store, cfg config.Config, log *slog.Logger) error {
	t := time.NewTicker(cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			now := time.Now().Unix()
			tracker.Poll(ctx, now)
			if err := st.Prune(ctx, now-int64(cfg.Retention.Seconds())); err != nil {
				log.Warn("snapshot prune failed", "err", err)
			}
		}
	}
}
