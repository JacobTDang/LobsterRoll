// Package config loads watcher-svc settings from the environment.
package config

import (
	"fmt"
	"os"
	"strings"
)

// Config is the resolved watcher-svc configuration.
type Config struct {
	RPCWSSURL       string // Polygon WebSocket RPC (required; must be ws/wss for subscriptions)
	NATSURL         string
	LeaderboardAddr string // gRPC address of leaderboard-svc
	DBPath          string
}

const (
	defNATSURL         = "nats://nats:4222"
	defLeaderboardAddr = "leaderboard:50051"
	defDBPath          = "watcher.db"
)

// Load resolves config using getenv, applying defaults and validating.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		RPCWSSURL:       getenv("RPC_WSS_URL"),
		NATSURL:         orDefault(getenv("NATS_URL"), defNATSURL),
		LeaderboardAddr: orDefault(getenv("LEADERBOARD_GRPC_ADDR"), defLeaderboardAddr),
		DBPath:          orDefault(getenv("WATCHER_DB_PATH"), defDBPath),
	}
	if cfg.RPCWSSURL == "" {
		return Config{}, fmt.Errorf("RPC_WSS_URL is required")
	}
	if s := cfg.RPCWSSURL; !strings.HasPrefix(s, "ws://") && !strings.HasPrefix(s, "wss://") {
		return Config{}, fmt.Errorf("RPC_WSS_URL must be a ws:// or wss:// endpoint (log subscriptions need WebSocket), got %q", s)
	}
	return cfg, nil
}

// LoadFromEnv loads config from the process environment.
func LoadFromEnv() (Config, error) { return Load(os.Getenv) }

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
