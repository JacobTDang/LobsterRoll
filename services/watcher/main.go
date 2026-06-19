package main

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/pkg/svc"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/backoff"
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

	// Polygon WebSocket client for live log subscription + backfill. Retry the
	// initial dial with backoff: providers (e.g. Alchemy) answer bursty
	// reconnects with HTTP 429, and an all-day run must self-heal rather than
	// exit on a transient throttle. The reconnect loop inside Run already backs
	// off; this guards the very first connect the same way.
	ec, err := dialWithRetry(ctx, cfg.RPCWSSURL, log)
	if err != nil {
		return err
	}
	defer ec.Close()

	set := watchset.New()
	seen := dedup.New()
	fd := feeder.New(lbClient, set, log)
	w := chainwatch.New(ec, set, seen, st, pub, log)

	g, ctx := errgroup.WithContext(ctx)
	ready := make(chan struct{}) // closed once the first watchset snapshot is applied
	g.Go(func() error { return fd.Run(ctx, ready) })
	g.Go(func() error {
		// Don't backfill against an empty watchset: wait for the first snapshot.
		select {
		case <-ready:
		case <-ctx.Done():
			return nil
		}
		log.Info("watchset ready; starting chain watcher")
		return w.Run(ctx)
	})
	return g.Wait()
}

// dialBackoff bounds the initial-connect retry. Deliberately gentle: providers
// rate-limit (429) the RATE of new WS connections, so retrying too eagerly keeps
// the limit hot and never recovers. Starting at 15s and capping at 5m means at
// most a handful of attempts before settling to one every 5 minutes — enough to
// reconnect once the provider's window resets without hammering it.
const (
	dialBackoffBase = 15 * time.Second
	dialBackoffMax  = 5 * time.Minute
)

// dialWithRetry dials the WS RPC, retrying transient failures (notably HTTP 429
// rate limits) with capped exponential backoff until it connects or ctx is
// cancelled.
func dialWithRetry(ctx context.Context, url string, log *slog.Logger) (*ethclient.Client, error) {
	for attempt := 0; ; attempt++ {
		ec, err := ethclient.DialContext(ctx, url)
		if err == nil {
			if attempt > 0 {
				log.Info("rpc connected after retry", "attempts", attempt+1)
			}
			return ec, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		d := backoff.Delay(attempt, dialBackoffBase, dialBackoffMax)
		log.Warn("rpc dial failed; retrying", "err", err, "attempt", attempt+1, "retry_in", d.String())
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(d):
		}
	}
}

// redactURL hides any API key embedded in an RPC URL's query string before logging.
func redactURL(url string) string {
	if i := strings.IndexByte(url, '?'); i >= 0 {
		return url[:i] + "?<redacted>"
	}
	return url
}
