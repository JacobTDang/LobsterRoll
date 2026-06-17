// Package config loads enrichment-svc settings from the environment.
package config

import (
	"os"

	"github.com/JacobTDang/LobsterRoll/services/enrichment/internal/client"
)

// Config is the resolved enrichment-svc configuration.
type Config struct {
	GammaBase string
	DBPath    string
	GRPCAddr  string
}

const (
	defDBPath   = "enrichment.db"
	defGRPCAddr = ":50052"
)

// Load resolves config using getenv, applying defaults.
func Load(getenv func(string) string) (Config, error) {
	return Config{
		GammaBase: orDefault(getenv("ENRICHMENT_GAMMA_BASE"), client.DefaultBaseURL),
		DBPath:    orDefault(getenv("ENRICHMENT_DB_PATH"), defDBPath),
		GRPCAddr:  orDefault(getenv("ENRICHMENT_GRPC_ADDR"), defGRPCAddr),
	}, nil
}

// LoadFromEnv loads config from the process environment.
func LoadFromEnv() (Config, error) { return Load(os.Getenv) }

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
