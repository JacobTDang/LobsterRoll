package config

import "testing"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"RPC_WSS_URL": "wss://poly.example/ws"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.NATSURL != defNATSURL || cfg.LeaderboardAddr != defLeaderboardAddr || cfg.DBPath != defDBPath {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestLoad_Overrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"RPC_WSS_URL":           "ws://localhost:8546",
		"NATS_URL":              "nats://localhost:4222",
		"LEADERBOARD_GRPC_ADDR": "localhost:50051",
		"WATCHER_DB_PATH":       "/data/w.db",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{
		RPCWSSURL: "ws://localhost:8546", NATSURL: "nats://localhost:4222",
		LeaderboardAddr: "localhost:50051", DBPath: "/data/w.db",
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
		{"missing rpc", map[string]string{}},
		{"http rpc not allowed", map[string]string{"RPC_WSS_URL": "https://poly.example"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(env(tt.env)); err == nil {
				t.Errorf("expected error for %s", tt.name)
			}
		})
	}
}
