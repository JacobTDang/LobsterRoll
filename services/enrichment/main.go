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
	"github.com/JacobTDang/LobsterRoll/services/enrichment/internal/cache"
	"github.com/JacobTDang/LobsterRoll/services/enrichment/internal/client"
	"github.com/JacobTDang/LobsterRoll/services/enrichment/internal/config"
	"github.com/JacobTDang/LobsterRoll/services/enrichment/internal/server"
)

func main() {
	svc.Run("enrichment", run)
}

func run(ctx context.Context, log *slog.Logger) error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	log.Info("config loaded", "gamma", cfg.GammaBase, "db", cfg.DBPath, "grpc", cfg.GRPCAddr)

	cch, err := cache.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer cch.Close()

	srv := server.New(cch, client.New(cfg.GammaBase, nil))

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return err
	}
	gs := grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{Time: 30 * time.Second, Timeout: 10 * time.Second}),
	)
	lobsterrollv1.RegisterEnrichmentServer(gs, srv)
	reflection.Register(gs)

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
	return g.Wait()
}
