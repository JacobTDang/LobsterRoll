package config

import (
	"testing"
	"time"

	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/client"
)

func envFunc(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load(envFunc(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		Metric:         client.MetricPNL,
		Window:         "30d",
		TopN:           30,
		APIBase:        client.DefaultBaseURL,
		DBPath:         "watchset.db",
		GRPCAddr:       ":50051",
		PricewatchAddr: "pricewatch:50053",

		DataAPIBase:        "https://data-api.polymarket.com",
		StatsMinResolved:   20,
		StatsMinWinRate:    0.90,
		StatsMinPortfolio:  100_000,
		StatsMinRealized:   0,
		StatsRequireFresh:  true,
		SkillShrinkK:       200,
		CandidateTopK:      50,
		StatsMaxCandidates: 100,
		StatsMaxActivity:   3000,
		StatsConcurrency:   8,
		StatsRefresh:       24 * time.Hour,
	}
	if cfg != want {
		t.Fatalf("defaults = %+v, want %+v", cfg, want)
	}
}

func TestLoad_Overrides(t *testing.T) {
	cfg, err := Load(envFunc(map[string]string{
		"LEADERBOARD_METRIC":        "volume",
		"LEADERBOARD_WINDOW":        "7d",
		"LEADERBOARD_TOP_N":         "10",
		"LEADERBOARD_API_BASE":      "http://localhost:9999",
		"LEADERBOARD_DB_PATH":       "/data/w.db",
		"LEADERBOARD_GRPC_ADDR":     ":7000",
		"PRICEWATCH_GRPC_ADDR":      "pricewatch:9999",
		"LEADERBOARD_DATA_API_BASE": "http://localhost:8888",
		"STATS_MIN_RESOLVED":        "5",
		"STATS_MIN_WIN_RATE":        "0.8",
		"STATS_MIN_PORTFOLIO_USD":   "50000",
		"STATS_MIN_REALIZED_PNL":    "25000",
		"STATS_REQUIRE_FRESH":       "false",
		"SKILL_SHRINKAGE_K":         "150",
		"CANDIDATE_TOPK":            "30",
		"STATS_MAX_CANDIDATES":      "40",
		"STATS_MAX_ACTIVITY_ROWS":   "1000",
		"STATS_CONCURRENCY":         "12",
		"STATS_REFRESH_INTERVAL":    "3h",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		Metric:         client.MetricVolume,
		Window:         "7d",
		TopN:           10,
		APIBase:        "http://localhost:9999",
		DBPath:         "/data/w.db",
		GRPCAddr:       ":7000",
		PricewatchAddr: "pricewatch:9999",

		DataAPIBase:        "http://localhost:8888",
		StatsMinResolved:   5,
		StatsMinWinRate:    0.8,
		StatsMinPortfolio:  50000,
		StatsMinRealized:   25000,
		StatsRequireFresh:  false,
		SkillShrinkK:       150,
		CandidateTopK:      30,
		StatsMaxCandidates: 40,
		StatsMaxActivity:   1000,
		StatsConcurrency:   12,
		StatsRefresh:       3 * time.Hour,
	}
	if cfg != want {
		t.Fatalf("overrides = %+v, want %+v", cfg, want)
	}
}

func TestLoad_Invalid(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"bad metric", map[string]string{"LEADERBOARD_METRIC": "roi"}},
		{"bad window", map[string]string{"LEADERBOARD_WINDOW": "weekly"}},
		{"non-numeric top_n", map[string]string{"LEADERBOARD_TOP_N": "lots"}},
		{"zero top_n", map[string]string{"LEADERBOARD_TOP_N": "0"}},
		{"non-numeric min resolved", map[string]string{"STATS_MIN_RESOLVED": "many"}},
		{"zero candidate topk", map[string]string{"CANDIDATE_TOPK": "0"}},
		{"zero max candidates", map[string]string{"STATS_MAX_CANDIDATES": "0"}},
		{"zero max activity", map[string]string{"STATS_MAX_ACTIVITY_ROWS": "0"}},
		{"zero concurrency", map[string]string{"STATS_CONCURRENCY": "0"}},
		{"bad win rate", map[string]string{"STATS_MIN_WIN_RATE": "1.5"}},
		{"non-numeric win rate", map[string]string{"STATS_MIN_WIN_RATE": "high"}},
		{"bad stats refresh", map[string]string{"STATS_REFRESH_INTERVAL": "soon"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(envFunc(tt.env)); err == nil {
				t.Errorf("expected error for %s", tt.name)
			}
		})
	}
}
