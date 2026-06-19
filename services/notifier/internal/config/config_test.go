package config

import (
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"TELEGRAM_BOT_TOKEN": "tok", "TELEGRAM_CHAT_ID": "42"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.NATSURL != defNATSURL || cfg.EnrichmentAddr != defEnrichmentAddr ||
		cfg.LeaderboardAddr != defLeaderboardAddr || cfg.QueueGroup != defQueueGroup ||
		cfg.AlertDedupTTL != defAlertDedupTTL || cfg.AlertCooldown != defAlertCooldown {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestLoad_Overrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"TELEGRAM_BOT_TOKEN":         "tok",
		"TELEGRAM_CHAT_ID":           "42",
		"TELEGRAM_BASE_URL":          "http://localhost:8099",
		"NATS_URL":                   "nats://localhost:4222",
		"ENRICHMENT_GRPC_ADDR":       "localhost:50052",
		"LEADERBOARD_GRPC_ADDR":      "localhost:50051",
		"NOTIFIER_QUEUE_GROUP":       "n2",
		"ALERT_DEDUP_TTL":            "1h",
		"ALERT_COOLDOWN":             "30m",
		"USER_WALLET":                "0xme",
		"DATA_API_BASE":              "http://localhost:8100",
		"MY_POSITIONS_POLL_INTERVAL": "2m",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		TelegramToken: "tok", TelegramChatID: "42", TelegramBaseURL: "http://localhost:8099",
		NATSURL: "nats://localhost:4222", EnrichmentAddr: "localhost:50052",
		LeaderboardAddr: "localhost:50051", QueueGroup: "n2", AlertDedupTTL: time.Hour,
		AlertCooldown: 30 * time.Minute,
		UserWallet:    "0xme", DataAPIBase: "http://localhost:8100", MyPositionsPoll: 2 * time.Minute,
	}
	if cfg != want {
		t.Fatalf("got %+v, want %+v", cfg, want)
	}
}

func TestLoad_Invalid(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"missing token", map[string]string{"TELEGRAM_CHAT_ID": "42"}},
		{"missing chat id", map[string]string{"TELEGRAM_BOT_TOKEN": "tok"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(env(tt.env)); err == nil {
				t.Errorf("expected error for %s", tt.name)
			}
		})
	}
}
