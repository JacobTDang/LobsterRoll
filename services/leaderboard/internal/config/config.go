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

	// Stats pipeline (per-wallet consistency stats + selection).
	DataAPIBase        string        // data-api host for per-wallet crawls
	StatsMinResolved   int           // exclude wallets with fewer resolved markets
	CandidateTopK      int           // top-K per window pulled into the candidate pool
	StatsMaxCandidates int           // cap on candidates crawled per refresh
	StatsMaxActivity   int           // cap on activity rows fetched per wallet
	StatsConcurrency   int           // max concurrent wallet crawls
	StatsRefresh       time.Duration // how often to rebuild the stats/watchset
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

	defDataAPIBase        = "https://data-api.polymarket.com"
	defStatsMinResolved   = 20
	defCandidateTopK      = 50
	defStatsMaxCandidates = 60
	defStatsMaxActivity   = 3000
	defStatsConcurrency   = 8
	defStatsRefresh       = 12 * time.Hour
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

		DataAPIBase:        orDefault(getenv("LEADERBOARD_DATA_API_BASE"), defDataAPIBase),
		StatsMinResolved:   defStatsMinResolved,
		CandidateTopK:      defCandidateTopK,
		StatsMaxCandidates: defStatsMaxCandidates,
		StatsMaxActivity:   defStatsMaxActivity,
		StatsConcurrency:   defStatsConcurrency,
		StatsRefresh:       defStatsRefresh,
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

	for _, p := range []struct {
		key string
		dst *int
	}{
		{"STATS_MIN_RESOLVED", &cfg.StatsMinResolved},
		{"CANDIDATE_TOPK", &cfg.CandidateTopK},
		{"STATS_MAX_CANDIDATES", &cfg.StatsMaxCandidates},
		{"STATS_MAX_ACTIVITY_ROWS", &cfg.StatsMaxActivity},
		{"STATS_CONCURRENCY", &cfg.StatsConcurrency},
	} {
		if v := getenv(p.key); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return Config{}, fmt.Errorf("%s: %w", p.key, err)
			}
			*p.dst = n
		}
	}
	if v := getenv("STATS_REFRESH_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("STATS_REFRESH_INTERVAL: %w", err)
		}
		cfg.StatsRefresh = d
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
	if cfg.StatsMinResolved < 0 {
		return Config{}, fmt.Errorf("STATS_MIN_RESOLVED must be >= 0, got %d", cfg.StatsMinResolved)
	}
	if cfg.CandidateTopK <= 0 {
		return Config{}, fmt.Errorf("CANDIDATE_TOPK must be > 0, got %d", cfg.CandidateTopK)
	}
	if cfg.StatsMaxCandidates <= 0 {
		return Config{}, fmt.Errorf("STATS_MAX_CANDIDATES must be > 0, got %d", cfg.StatsMaxCandidates)
	}
	if cfg.StatsMaxActivity <= 0 {
		return Config{}, fmt.Errorf("STATS_MAX_ACTIVITY_ROWS must be > 0, got %d", cfg.StatsMaxActivity)
	}
	if cfg.StatsConcurrency <= 0 {
		return Config{}, fmt.Errorf("STATS_CONCURRENCY must be > 0, got %d", cfg.StatsConcurrency)
	}
	if cfg.StatsRefresh <= 0 {
		return Config{}, fmt.Errorf("STATS_REFRESH_INTERVAL must be > 0, got %s", cfg.StatsRefresh)
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
