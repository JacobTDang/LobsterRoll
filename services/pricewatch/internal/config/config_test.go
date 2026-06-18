package config

import (
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.NATSURL != defNATSURL || cfg.DBPath != defDBPath || cfg.QueueGroup != defQueueGroup ||
		cfg.PollInterval != defPollInterval || cfg.TokenTTL != defTokenTTL || cfg.Retention != defRetention ||
		cfg.EnrichmentAddr != defEnrichmentAddr || cfg.GRPCAddr != defGRPCAddr || cfg.CloseBuffer != defCloseBuffer || cfg.SettleInterval != defSettleInterval {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestLoad_Overrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"NATS_URL":                 "nats://localhost:4222",
		"PRICEWATCH_DB_PATH":       "/data/p.db",
		"PRICEWATCH_POLL_INTERVAL": "30s",
		"PRICEWATCH_TOKEN_TTL":     "12h",
		"PRICEWATCH_RETENTION":     "7d",
	}))
	// 7d isn't a valid Go duration -> must error.
	if err == nil {
		t.Fatal("expected error: '7d' is not a valid duration")
	}

	cfg, err = Load(env(map[string]string{
		"PRICEWATCH_POLL_INTERVAL": "30s",
		"PRICEWATCH_TOKEN_TTL":     "12h",
		"PRICEWATCH_RETENTION":     "168h",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PollInterval != 30*time.Second || cfg.TokenTTL != 12*time.Hour || cfg.Retention != 168*time.Hour {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestLoad_Invalid(t *testing.T) {
	for _, k := range []string{"PRICEWATCH_POLL_INTERVAL", "PRICEWATCH_TOKEN_TTL", "PRICEWATCH_RETENTION", "PRICEWATCH_CLOSE_BUFFER", "PRICEWATCH_SETTLE_INTERVAL"} {
		if _, err := Load(env(map[string]string{k: "0s"})); err == nil {
			t.Errorf("%s=0s should be rejected", k)
		}
	}
}
