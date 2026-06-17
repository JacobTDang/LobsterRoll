// Package config loads notifier-svc settings from the environment.
package config

import (
	"fmt"
	"os"
)

// Config is the resolved notifier-svc configuration.
type Config struct {
	TelegramToken  string
	TelegramChatID string
	NATSURL        string
	EnrichmentAddr string
	QueueGroup     string
}

const (
	defNATSURL        = "nats://nats:4222"
	defEnrichmentAddr = "enrichment:50052"
	defQueueGroup     = "notifier"
)

// Load resolves config using getenv, applying defaults and validating.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		TelegramToken:  getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID: getenv("TELEGRAM_CHAT_ID"),
		NATSURL:        orDefault(getenv("NATS_URL"), defNATSURL),
		EnrichmentAddr: orDefault(getenv("ENRICHMENT_GRPC_ADDR"), defEnrichmentAddr),
		QueueGroup:     orDefault(getenv("NOTIFIER_QUEUE_GROUP"), defQueueGroup),
	}
	if cfg.TelegramToken == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.TelegramChatID == "" {
		return Config{}, fmt.Errorf("TELEGRAM_CHAT_ID is required")
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
