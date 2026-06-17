// Package config loads leaderboard-svc settings from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/client"
)

// Config is the resolved leaderboard-svc configuration.
type Config struct {
	Metric   client.Metric
	Window   client.Window
	TopN     int
	Interval time.Duration
	APIBase  string
	DBPath   string
	GRPCAddr string
}

// Defaults (also documented in .env.example). Per project decision the default
// window is 30d to favor consistent performers over short-term spikes.
const (
	defWindow   = "30d"
	defMetric   = "pnl"
	defTopN     = 25
	defInterval = 6 * time.Hour
	defAPIBase  = client.DefaultBaseURL
	defDBPath   = "watchset.db"
	defGRPCAddr = ":50051"
)

// Load resolves config using getenv (e.g. os.Getenv), applying defaults and
// validating every field.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Metric:   client.Metric(orDefault(getenv("LEADERBOARD_METRIC"), defMetric)),
		Window:   client.Window(orDefault(getenv("LEADERBOARD_WINDOW"), defWindow)),
		TopN:     defTopN,
		Interval: defInterval,
		APIBase:  orDefault(getenv("LEADERBOARD_API_BASE"), defAPIBase),
		DBPath:   orDefault(getenv("LEADERBOARD_DB_PATH"), defDBPath),
		GRPCAddr: orDefault(getenv("LEADERBOARD_GRPC_ADDR"), defGRPCAddr),
	}

	if v := getenv("LEADERBOARD_TOP_N"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("LEADERBOARD_TOP_N: %w", err)
		}
		cfg.TopN = n
	}
	if v := getenv("LEADERBOARD_SYNC_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("LEADERBOARD_SYNC_INTERVAL: %w", err)
		}
		cfg.Interval = d
	}

	if cfg.Metric != client.MetricPNL && cfg.Metric != client.MetricVolume {
		return Config{}, fmt.Errorf("LEADERBOARD_METRIC %q: want pnl or volume", cfg.Metric)
	}
	if !client.ValidWindow(cfg.Window) {
		return Config{}, fmt.Errorf("LEADERBOARD_WINDOW %q: want 1d, 7d, 30d, or all", cfg.Window)
	}
	if cfg.TopN <= 0 {
		return Config{}, fmt.Errorf("LEADERBOARD_TOP_N must be > 0, got %d", cfg.TopN)
	}
	if cfg.Interval <= 0 {
		return Config{}, fmt.Errorf("LEADERBOARD_SYNC_INTERVAL must be > 0, got %s", cfg.Interval)
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
