// Package config loads pricewatch-svc settings from the environment.
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/JacobTDang/LobsterRoll/services/pricewatch/internal/client"
)

// Config is the resolved pricewatch-svc configuration.
type Config struct {
	NATSURL      string
	CLOBBase     string
	DBPath       string
	QueueGroup   string
	PollInterval time.Duration // how often to snapshot every tracked token
	TokenTTL     time.Duration // stop polling a token untraded this long (market resolved)
	Retention    time.Duration // prune snapshots older than this
}

const (
	defNATSURL      = "nats://nats:4222"
	defDBPath       = "pricewatch.db"
	defQueueGroup   = "pricewatch"
	defPollInterval = 2 * time.Minute
	defTokenTTL     = 48 * time.Hour
	defRetention    = 30 * 24 * time.Hour
)

// Load resolves config using getenv, applying defaults and validating.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		NATSURL:      orDefault(getenv("NATS_URL"), defNATSURL),
		CLOBBase:     orDefault(getenv("PRICEWATCH_CLOB_BASE"), client.DefaultBaseURL),
		DBPath:       orDefault(getenv("PRICEWATCH_DB_PATH"), defDBPath),
		QueueGroup:   orDefault(getenv("PRICEWATCH_QUEUE_GROUP"), defQueueGroup),
		PollInterval: defPollInterval,
		TokenTTL:     defTokenTTL,
		Retention:    defRetention,
	}
	for _, p := range []struct {
		key string
		dst *time.Duration
	}{
		{"PRICEWATCH_POLL_INTERVAL", &cfg.PollInterval},
		{"PRICEWATCH_TOKEN_TTL", &cfg.TokenTTL},
		{"PRICEWATCH_RETENTION", &cfg.Retention},
	} {
		if v := getenv(p.key); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				return Config{}, fmt.Errorf("%s: %w", p.key, err)
			}
			*p.dst = d
		}
	}
	if cfg.PollInterval <= 0 {
		return Config{}, fmt.Errorf("PRICEWATCH_POLL_INTERVAL must be > 0, got %s", cfg.PollInterval)
	}
	if cfg.TokenTTL <= 0 {
		return Config{}, fmt.Errorf("PRICEWATCH_TOKEN_TTL must be > 0, got %s", cfg.TokenTTL)
	}
	if cfg.Retention <= 0 {
		return Config{}, fmt.Errorf("PRICEWATCH_RETENTION must be > 0, got %s", cfg.Retention)
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
