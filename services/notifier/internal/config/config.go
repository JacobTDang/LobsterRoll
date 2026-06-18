// Package config loads notifier-svc settings from the environment.
package config

import (
	"fmt"
	"os"
	"time"
)

// Config is the resolved notifier-svc configuration.
type Config struct {
	TelegramToken   string
	TelegramChatID  string
	TelegramBaseURL string // override the Bot API host (tests/verify); "" = default
	NATSURL         string
	EnrichmentAddr  string
	LeaderboardAddr string
	QueueGroup      string
	AlertDedupTTL   time.Duration // suppress duplicate trade alerts for this long
}

const (
	defNATSURL         = "nats://nats:4222"
	defEnrichmentAddr  = "enrichment:50052"
	defLeaderboardAddr = "leaderboard:50051"
	defQueueGroup      = "notifier"
	defAlertDedupTTL   = 24 * time.Hour
)

// Load resolves config using getenv, applying defaults and validating.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		TelegramToken:   getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID:  getenv("TELEGRAM_CHAT_ID"),
		TelegramBaseURL: getenv("TELEGRAM_BASE_URL"),
		NATSURL:         orDefault(getenv("NATS_URL"), defNATSURL),
		EnrichmentAddr:  orDefault(getenv("ENRICHMENT_GRPC_ADDR"), defEnrichmentAddr),
		LeaderboardAddr: orDefault(getenv("LEADERBOARD_GRPC_ADDR"), defLeaderboardAddr),
		QueueGroup:      orDefault(getenv("NOTIFIER_QUEUE_GROUP"), defQueueGroup),
		AlertDedupTTL:   defAlertDedupTTL,
	}
	if v := getenv("ALERT_DEDUP_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("ALERT_DEDUP_TTL: %w", err)
		}
		cfg.AlertDedupTTL = d
	}
	if cfg.TelegramToken == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.TelegramChatID == "" {
		return Config{}, fmt.Errorf("TELEGRAM_CHAT_ID is required")
	}
	if cfg.AlertDedupTTL <= 0 {
		return Config{}, fmt.Errorf("ALERT_DEDUP_TTL must be > 0, got %s", cfg.AlertDedupTTL)
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
