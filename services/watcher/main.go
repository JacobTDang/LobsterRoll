package main

import (
	"context"
	"log/slog"
	"strings"

	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/pkg/svc"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/chainwatch"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/config"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/dedup"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/feeder"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/store"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/watchset"
)

func main() {
	svc.Run("watcher", run)
}

func run(ctx context.Context, log *slog.Logger) error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	log.Info("config loaded", "rpc", redactURL(cfg.RPCWSSURL), "nats", cfg.NATSURL,
		"leaderboard", cfg.LeaderboardAddr, "db", cfg.DBPath)

	// Persistent cursor (last processed block).
	st, err := store.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	// NATS publisher (fail-fast at startup, reconnect mid-run).
	pub, err := bus.Connect(cfg.NATSURL)
	if err != nil {
		return err
	}
	defer pub.Close()

	// Leaderboard gRPC client feeding the watchset.
	conn, err := grpc.NewClient(cfg.LeaderboardAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	lbClient := lobsterrollv1.NewLeaderboardClient(conn)

	// Polygon WebSocket client for live log subscription + backfill.
	ec, err := ethclient.DialContext(ctx, cfg.RPCWSSURL)
	if err != nil {
		return err
	}
	defer ec.Close()

	set := watchset.New()
	seen := dedup.New()
	fd := feeder.New(lbClient, set, log)
	w := chainwatch.New(ec, set, seen, st, pub, log)

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return fd.Run(ctx) })   // keep watchset in sync
	g.Go(func() error { return w.Run(ctx) })    // detect + publish trades
	return g.Wait()
}

// redactURL hides any API key embedded in an RPC URL's query string before logging.
func redactURL(url string) string {
	if i := strings.IndexByte(url, '?'); i >= 0 {
		return url[:i] + "?<redacted>"
	}
	return url
}
