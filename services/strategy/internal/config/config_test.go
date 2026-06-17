package config

import (
	"testing"

	"github.com/JacobTDang/LobsterRoll/services/strategy/internal/decide"
	"github.com/JacobTDang/LobsterRoll/services/strategy/internal/marketdata"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Policy.Sizing != decide.SizingFixed || cfg.Policy.FixedUSD != 10 {
		t.Errorf("sizing defaults: %+v", cfg.Policy)
	}
	if cfg.Policy.MaxSlippage != 0.03 { // 3 cents
		t.Errorf("MaxSlippage = %v, want 0.03", cfg.Policy.MaxSlippage)
	}
	if cfg.NATSURL != defNATSURL || cfg.GammaBase != marketdata.DefaultBaseURL || cfg.QueueGroup != defQueueGroup {
		t.Errorf("infra defaults: %+v", cfg)
	}
	if cfg.Allowlist != nil {
		t.Errorf("default allowlist should be nil (allow all), got %v", cfg.Allowlist)
	}
}

func TestLoad_Overrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"STRATEGY_SIZING":            "proportional",
		"STRATEGY_PROPORTION":        "0.1",
		"STRATEGY_FIXED_USD":         "50",
		"STRATEGY_MIN_SIZE_USD":      "2",
		"STRATEGY_MAX_SIZE_USD":      "200",
		"STRATEGY_MIN_LIQUIDITY_USD": "5000",
		"MAX_SLIPPAGE_CENTS":         "5",
		"STRATEGY_ALLOWLIST":         "0xAAA, 0xbbb ,",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Policy.Sizing != decide.SizingProportional || cfg.Policy.Proportion != 0.1 {
		t.Errorf("policy = %+v", cfg.Policy)
	}
	if cfg.Policy.MaxSlippage != 0.05 {
		t.Errorf("MaxSlippage = %v, want 0.05", cfg.Policy.MaxSlippage)
	}
	if len(cfg.Allowlist) != 2 || !cfg.Allowlist["0xaaa"] || !cfg.Allowlist["0xbbb"] {
		t.Errorf("allowlist = %v (want lowercased 0xaaa,0xbbb)", cfg.Allowlist)
	}
}

func TestLoad_Invalid(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"bad sizing", map[string]string{"STRATEGY_SIZING": "huge"}},
		{"bad number", map[string]string{"STRATEGY_FIXED_USD": "lots"}},
		{"zero max size", map[string]string{"STRATEGY_MAX_SIZE_USD": "0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(env(tt.env)); err == nil {
				t.Errorf("expected error for %s", tt.name)
			}
		})
	}
}
