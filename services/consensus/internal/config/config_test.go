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
	if cfg.NATSURL != defNATSURL {
		t.Errorf("NATSURL=%q want %q", cfg.NATSURL, defNATSURL)
	}
	if cfg.MinWallets != defMinWallets {
		t.Errorf("MinWallets=%d want %d", cfg.MinWallets, defMinWallets)
	}
	if cfg.Window != defWindow {
		t.Errorf("Window=%s want %s", cfg.Window, defWindow)
	}
	if cfg.DBPath != defDBPath {
		t.Errorf("DBPath=%q want %q", cfg.DBPath, defDBPath)
	}
	if cfg.QueueGroup != defQueueGroup {
		t.Errorf("QueueGroup=%q want %q", cfg.QueueGroup, defQueueGroup)
	}
}

func TestLoad_Overrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"NATS_URL":              "nats://localhost:4222",
		"CONSENSUS_MIN_WALLETS": "5",
		"CONSENSUS_WINDOW":      "90m",
		"CONSENSUS_DB_PATH":     "/data/c.db",
		"CONSENSUS_QUEUE_GROUP": "cons2",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.NATSURL != "nats://localhost:4222" {
		t.Errorf("NATSURL=%q", cfg.NATSURL)
	}
	if cfg.MinWallets != 5 {
		t.Errorf("MinWallets=%d want 5", cfg.MinWallets)
	}
	if cfg.Window != 90*time.Minute {
		t.Errorf("Window=%s want 90m", cfg.Window)
	}
	if cfg.DBPath != "/data/c.db" {
		t.Errorf("DBPath=%q", cfg.DBPath)
	}
	if cfg.QueueGroup != "cons2" {
		t.Errorf("QueueGroup=%q", cfg.QueueGroup)
	}
}

func TestLoad_Invalid(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"bad min wallets", map[string]string{"CONSENSUS_MIN_WALLETS": "lots"}},
		{"min wallets too small", map[string]string{"CONSENSUS_MIN_WALLETS": "1"}},
		{"bad window", map[string]string{"CONSENSUS_WINDOW": "soon"}},
		{"zero window", map[string]string{"CONSENSUS_WINDOW": "0s"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(env(tt.env)); err == nil {
				t.Errorf("expected error for %s", tt.name)
			}
		})
	}
}
