// Package config loads enrichment-svc settings from the environment.
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/JacobTDang/LobsterRoll/services/enrichment/internal/client"
)

// Config is the resolved enrichment-svc configuration.
type Config struct {
	GammaBase string
	DBPath    string
	GRPCAddr  string
	CacheTTL  time.Duration // re-fetch cached enrichments older than this; 0 = never
}

const (
	defDBPath   = "enrichment.db"
	defGRPCAddr = ":50052"
	defCacheTTL = 24 * time.Hour
)

// Load resolves config using getenv, applying defaults.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		GammaBase: orDefault(getenv("ENRICHMENT_GAMMA_BASE"), client.DefaultBaseURL),
		DBPath:    orDefault(getenv("ENRICHMENT_DB_PATH"), defDBPath),
		GRPCAddr:  orDefault(getenv("ENRICHMENT_GRPC_ADDR"), defGRPCAddr),
		CacheTTL:  defCacheTTL,
	}
	if v := getenv("ENRICHMENT_CACHE_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("ENRICHMENT_CACHE_TTL: %w", err)
		}
		cfg.CacheTTL = d
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
