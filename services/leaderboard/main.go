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
		"interval", cfg.Interval, "grpc", cfg.GRPCAddr, "db", cfg.DBPath)

	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	srv := server.New(st)
	sy := syncer.New(client.New(cfg.APIBase, nil), st, srv,
		cfg.Metric, cfg.Window, cfg.TopN, cfg.Interval, log)

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
