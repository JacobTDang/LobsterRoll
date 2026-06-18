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
	NATSURL        string
	CLOBBase       string
	EnrichmentAddr string // dial enrichment-svc for market end dates (CLV settling)
	DBPath         string
	QueueGroup     string
	PollInterval   time.Duration // how often to snapshot every tracked token
	TokenTTL       time.Duration // stop polling a token untraded this long (market resolved)
	Retention      time.Duration // prune snapshots older than this
	CloseBuffer    time.Duration // CLV close = snapshot near (endDate - CloseBuffer)
	SettleInterval time.Duration // how often to compute CLV for resolved trades
}

const (
	defNATSURL        = "nats://nats:4222"
	defDBPath         = "pricewatch.db"
	defQueueGroup     = "pricewatch"
	defEnrichmentAddr = "enrichment:50052"
	defPollInterval   = 2 * time.Minute
	defTokenTTL       = 7 * 24 * time.Hour // conservative: keep quiet-but-live markets polling
	defRetention      = 30 * 24 * time.Hour
	defCloseBuffer    = 4 * time.Hour
	defSettleInterval = time.Hour
)

// Load resolves config using getenv, applying defaults and validating.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		NATSURL:        orDefault(getenv("NATS_URL"), defNATSURL),
		CLOBBase:       orDefault(getenv("PRICEWATCH_CLOB_BASE"), client.DefaultBaseURL),
		EnrichmentAddr: orDefault(getenv("ENRICHMENT_GRPC_ADDR"), defEnrichmentAddr),
		DBPath:         orDefault(getenv("PRICEWATCH_DB_PATH"), defDBPath),
		QueueGroup:     orDefault(getenv("PRICEWATCH_QUEUE_GROUP"), defQueueGroup),
		PollInterval:   defPollInterval,
		TokenTTL:       defTokenTTL,
		Retention:      defRetention,
		CloseBuffer:    defCloseBuffer,
		SettleInterval: defSettleInterval,
	}
	for _, p := range []struct {
		key string
		dst *time.Duration
	}{
		{"PRICEWATCH_POLL_INTERVAL", &cfg.PollInterval},
		{"PRICEWATCH_TOKEN_TTL", &cfg.TokenTTL},
		{"PRICEWATCH_RETENTION", &cfg.Retention},
		{"PRICEWATCH_CLOSE_BUFFER", &cfg.CloseBuffer},
		{"PRICEWATCH_SETTLE_INTERVAL", &cfg.SettleInterval},
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
	if cfg.CloseBuffer <= 0 {
		return Config{}, fmt.Errorf("PRICEWATCH_CLOSE_BUFFER must be > 0, got %s", cfg.CloseBuffer)
	}
	if cfg.SettleInterval <= 0 {
		return Config{}, fmt.Errorf("PRICEWATCH_SETTLE_INTERVAL must be > 0, got %s", cfg.SettleInterval)
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
