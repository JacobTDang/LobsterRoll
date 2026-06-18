package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/pkg/svc"
	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/client"
	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/config"
	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/dataapi"
	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/server"
	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/store"
	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/syncer"
)

func main() {
	svc.Run("leaderboard", run)
}

func run(ctx context.Context, log *slog.Logger) error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	log.Info("config loaded",
		"metric", cfg.Metric, "window", cfg.Window, "topN", cfg.TopN,
		"grpc", cfg.GRPCAddr, "db", cfg.DBPath,
		"dataAPI", cfg.DataAPIBase, "statsRefresh", cfg.StatsRefresh,
		"minResolved", cfg.StatsMinResolved, "candidateTopK", cfg.CandidateTopK,
		"maxCandidates", cfg.StatsMaxCandidates)

	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	srv := server.New(st)
	// The watchset is driven by the consistency-stats pipeline: build a
	// candidate pool from the leaderboard, crawl each candidate's data-api
	// history into win-rate/PnL stats, then select the most consistent top-N.
	sy := syncer.NewStats(
		client.New(cfg.APIBase, nil),
		dataapi.New(cfg.DataAPIBase, nil),
		st, srv,
		syncer.StatsConfig{
			Metric:          cfg.Metric,
			CandidateTopK:   cfg.CandidateTopK,
			MaxCandidates:   cfg.StatsMaxCandidates,
			MaxActivity:     cfg.StatsMaxActivity,
			MinResolved:     cfg.StatsMinResolved,
			MinWinRate:      cfg.StatsMinWinRate,
			MinPortfolioUSD: cfg.StatsMinPortfolio,
			MinRealizedPnL:  cfg.StatsMinRealized,
			ShrinkK:         cfg.SkillShrinkK,
			TopN:            cfg.TopN,
			Interval:        cfg.StatsRefresh,
			Concurrency:     cfg.StatsConcurrency,
		},
		log)

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return err
	}
	// Keepalive reaps half-open StreamWatchset clients promptly so their
	// server-side goroutines/subscriptions don't leak.
	gs := grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	lobsterrollv1.RegisterLeaderboardServer(gs, srv)
	reflection.Register(gs) // enables grpcurl/ops introspection

	g, ctx := errgroup.WithContext(ctx)

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
	g.Go(func() error {
		return sy.Run(ctx)
	})

	return g.Wait()
}
