package config

import (
	"testing"

	cfg "github.com/JacobTDang/LobsterRoll/pkg/config"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

var base = map[string]string{
	"TRADER_PRIVATE_KEY":        "abc123",
	"POLYMARKET_API_KEY":        "k",
	"POLYMARKET_API_SECRET":     "s",
	"POLYMARKET_API_PASSPHRASE": "p",
}

func with(extra map[string]string) map[string]string {
	m := map[string]string{}
	for k, v := range base {
		m[k] = v
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestLoad_Defaults(t *testing.T) {
	c, err := Load(env(base))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.PerTradeUSD != 25 || c.PerDayUSD != 200 || c.ExposureUSD != 500 {
		t.Errorf("cap defaults = %+v", c)
	}
	if c.Policy.Mode != cfg.ModeApproval {
		t.Errorf("policy = %v, want approval", c.Policy.Mode)
	}
	if c.ExchangeAddress == "" || c.NATSURL != defNATSURL || c.QueueGroup != defQueueGroup {
		t.Errorf("infra defaults = %+v", c)
	}
}

func TestLoad_Overrides(t *testing.T) {
	c, err := Load(env(with(map[string]string{
		"EXECUTION_MODE":        "auto_below:50",
		"MAX_USD_PER_TRADE":     "10",
		"MAX_USD_PER_DAY":       "100",
		"MAX_OPEN_EXPOSURE_USD": "300",
		"TRADER_SIGNATURE_TYPE": "2",
	})))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Policy.Mode != cfg.ModeAutoBelow || c.Policy.CeilingUSD != 50 {
		t.Errorf("policy = %+v", c.Policy)
	}
	if c.PerTradeUSD != 10 || c.SignatureType != 2 {
		t.Errorf("overrides = %+v", c)
	}
}

func TestLoad_Invalid(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"missing key", map[string]string{"POLYMARKET_API_KEY": "k", "POLYMARKET_API_SECRET": "s", "POLYMARKET_API_PASSPHRASE": "p"}},
		{"missing creds", map[string]string{"TRADER_PRIVATE_KEY": "x"}},
		{"bad exec mode", with(map[string]string{"EXECUTION_MODE": "yolo"})},
		{"bad cap", with(map[string]string{"MAX_USD_PER_TRADE": "nope"})},
		{"zero cap", with(map[string]string{"MAX_USD_PER_DAY": "0"})},
		{"bad sig type", with(map[string]string{"TRADER_SIGNATURE_TYPE": "9"})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Load(env(tt.env)); err == nil {
				t.Errorf("expected error for %s", tt.name)
			}
		})
	}
}
