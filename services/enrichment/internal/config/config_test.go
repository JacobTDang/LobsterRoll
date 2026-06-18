package config

import (
	"testing"
	"time"

	"github.com/JacobTDang/LobsterRoll/services/enrichment/internal/client"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{GammaBase: client.DefaultBaseURL, DBPath: "enrichment.db", GRPCAddr: ":50052", CacheTTL: 24 * time.Hour}
	if cfg != want {
		t.Fatalf("got %+v, want %+v", cfg, want)
	}
}

func TestLoad_Overrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"ENRICHMENT_GAMMA_BASE": "http://localhost:9",
		"ENRICHMENT_DB_PATH":    "/data/e.db",
		"ENRICHMENT_GRPC_ADDR":  ":7001",
		"ENRICHMENT_CACHE_TTL":  "30m",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Config{GammaBase: "http://localhost:9", DBPath: "/data/e.db", GRPCAddr: ":7001", CacheTTL: 30 * time.Minute}
	if cfg != want {
		t.Fatalf("got %+v, want %+v", cfg, want)
	}
}

func TestLoad_BadCacheTTL(t *testing.T) {
	if _, err := Load(env(map[string]string{"ENRICHMENT_CACHE_TTL": "soon"})); err == nil {
		t.Fatal("expected error for bad ENRICHMENT_CACHE_TTL")
	}
}
