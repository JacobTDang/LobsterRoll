package config

import "testing"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"TELEGRAM_BOT_TOKEN": "tok", "TELEGRAM_CHAT_ID": "42"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.NATSURL != defNATSURL || cfg.EnrichmentAddr != defEnrichmentAddr || cfg.QueueGroup != defQueueGroup {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestLoad_Overrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"TELEGRAM_BOT_TOKEN":    "tok",
		"TELEGRAM_CHAT_ID":      "42",
		"NATS_URL":              "nats://localhost:4222",
		"ENRICHMENT_GRPC_ADDR":  "localhost:50052",
		"NOTIFIER_QUEUE_GROUP":  "n2",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		TelegramToken: "tok", TelegramChatID: "42", NATSURL: "nats://localhost:4222",
		EnrichmentAddr: "localhost:50052", QueueGroup: "n2",
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
