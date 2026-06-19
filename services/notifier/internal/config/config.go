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
	AlertDedupTTL   time.Duration // suppress duplicate (same-fill) trade alerts for this long
	AlertCooldown   time.Duration // collapse repeated wallet+market+side alerts; 0 = off
	UserWallet      string        // public wallet to track for position-exit alerts; "" = disabled
	DataAPIBase     string        // Polymarket data-api host (positions); "" = default
	MyPositionsPoll time.Duration // how often to refresh the user's positions
}

const (
	defNATSURL         = "nats://nats:4222"
	defEnrichmentAddr  = "enrichment:50052"
	defLeaderboardAddr = "leaderboard:50051"
	defQueueGroup      = "notifier"
	defAlertDedupTTL   = 24 * time.Hour
	defAlertCooldown   = 15 * time.Minute
	defMyPositionsPoll = 5 * time.Minute
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
		AlertCooldown:   defAlertCooldown,
		UserWallet:      getenv("USER_WALLET"),
		DataAPIBase:     getenv("DATA_API_BASE"),
		MyPositionsPoll: defMyPositionsPoll,
	}
	if v := getenv("MY_POSITIONS_POLL_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("MY_POSITIONS_POLL_INTERVAL: %w", err)
		}
		cfg.MyPositionsPoll = d
	}
	if v := getenv("ALERT_DEDUP_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("ALERT_DEDUP_TTL: %w", err)
		}
		cfg.AlertDedupTTL = d
	}
	if v := getenv("ALERT_COOLDOWN"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("ALERT_COOLDOWN: %w", err)
		}
		cfg.AlertCooldown = d
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
	if cfg.AlertCooldown < 0 {
		return Config{}, fmt.Errorf("ALERT_COOLDOWN must be >= 0, got %s", cfg.AlertCooldown)
	}
	if cfg.UserWallet != "" && cfg.MyPositionsPoll <= 0 {
		return Config{}, fmt.Errorf("MY_POSITIONS_POLL_INTERVAL must be > 0 when USER_WALLET is set, got %s", cfg.MyPositionsPoll)
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
