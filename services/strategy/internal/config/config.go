// Package config loads strategy-svc settings from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/JacobTDang/LobsterRoll/services/strategy/internal/decide"
	"github.com/JacobTDang/LobsterRoll/services/strategy/internal/marketdata"
)

// Config is the resolved strategy-svc configuration.
type Config struct {
	Policy     decide.Policy
	Allowlist  map[string]bool // condition ids; empty => allow all
	NATSURL    string
	GammaBase  string
	QueueGroup string
}

const (
	defNATSURL    = "nats://nats:4222"
	defQueueGroup = "strategy"
)

// Load resolves config using getenv, applying defaults and validating.
func Load(getenv func(string) string) (Config, error) {
	sizingStr := orDefault(getenv("STRATEGY_SIZING"), "fixed")
	var sizing decide.SizingMode
	switch strings.ToLower(sizingStr) {
	case "fixed":
		sizing = decide.SizingFixed
	case "proportional":
		sizing = decide.SizingProportional
	default:
		return Config{}, fmt.Errorf("STRATEGY_SIZING %q: want fixed or proportional", sizingStr)
	}

	fixedUSD, err := floatEnv(getenv, "STRATEGY_FIXED_USD", 10)
	if err != nil {
		return Config{}, err
	}
	proportion, err := floatEnv(getenv, "STRATEGY_PROPORTION", 0.05)
	if err != nil {
		return Config{}, err
	}
	minSize, err := floatEnv(getenv, "STRATEGY_MIN_SIZE_USD", 5)
	if err != nil {
		return Config{}, err
	}
	maxSize, err := floatEnv(getenv, "STRATEGY_MAX_SIZE_USD", 25)
	if err != nil {
		return Config{}, err
	}
	minLiq, err := floatEnv(getenv, "STRATEGY_MIN_LIQUIDITY_USD", 1000)
	if err != nil {
		return Config{}, err
	}
	slipCents, err := floatEnv(getenv, "MAX_SLIPPAGE_CENTS", 3)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Policy: decide.Policy{
			Sizing:          sizing,
			FixedUSD:        fixedUSD,
			Proportion:      proportion,
			MinSizeUSD:      minSize,
			MaxSizeUSD:      maxSize,
			MaxSlippage:     slipCents / 100.0,
			MinLiquidityUSD: minLiq,
		},
		Allowlist:  parseAllowlist(getenv("STRATEGY_ALLOWLIST")),
		NATSURL:    orDefault(getenv("NATS_URL"), defNATSURL),
		GammaBase:  orDefault(getenv("STRATEGY_GAMMA_BASE"), marketdata.DefaultBaseURL),
		QueueGroup: orDefault(getenv("STRATEGY_QUEUE_GROUP"), defQueueGroup),
	}
	if cfg.Policy.MaxSizeUSD <= 0 {
		return Config{}, fmt.Errorf("STRATEGY_MAX_SIZE_USD must be > 0")
	}
	return cfg, nil
}

// LoadFromEnv loads config from the process environment.
func LoadFromEnv() (Config, error) { return Load(os.Getenv) }

func parseAllowlist(s string) map[string]bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	m := make(map[string]bool)
	for _, part := range strings.Split(s, ",") {
		if id := strings.ToLower(strings.TrimSpace(part)); id != "" {
			m[id] = true
		}
	}
	return m
}

func floatEnv(getenv func(string) string, key string, def float64) (float64, error) {
	v := getenv(key)
	if v == "" {
		return def, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return f, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
