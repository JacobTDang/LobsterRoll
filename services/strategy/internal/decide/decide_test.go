package decide

import (
	"testing"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
)

func TestSizeUSD(t *testing.T) {
	tr := bus.TradeDetected{Size: "100", Price: "0.50"} // whale notional = $50
	tests := []struct {
		name string
		p    Policy
		want float64
	}{
		{"fixed", Policy{Sizing: SizingFixed, FixedUSD: 10}, 10},
		{"proportional 10%", Policy{Sizing: SizingProportional, Proportion: 0.10, MaxSizeUSD: 100}, 5},
		{"proportional clamped to max", Policy{Sizing: SizingProportional, Proportion: 0.50, MaxSizeUSD: 20}, 20},
		{"fixed clamped to max", Policy{Sizing: SizingFixed, FixedUSD: 75, MaxSizeUSD: 50}, 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SizeUSD(tr, tt.p)
			if err != nil {
				t.Fatalf("SizeUSD: %v", err)
			}
			if got != tt.want {
				t.Errorf("SizeUSD = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWithinSlippage(t *testing.T) {
	const slip = 0.03
	tests := []struct {
		side                    string
		whale, current          float64
		want                    bool
	}{
		{"buy", 0.50, 0.52, true},   // current within +3c
		{"buy", 0.50, 0.53, true},   // exactly +3c
		{"buy", 0.50, 0.54, false},  // +4c, too high
		{"sell", 0.50, 0.48, true},  // within -3c
		{"sell", 0.50, 0.47, true},  // exactly -3c
		{"sell", 0.50, 0.46, false}, // -4c, too low
		{"weird", 0.50, 0.50, false},
	}
	for _, tt := range tests {
		if got := WithinSlippage(tt.side, tt.whale, tt.current, slip); got != tt.want {
			t.Errorf("WithinSlippage(%s,%v,%v) = %v, want %v", tt.side, tt.whale, tt.current, got, tt.want)
		}
	}
}

var goodTrade = bus.TradeDetected{
	Wallet: "0xwhale", TokenID: "tok", Side: "buy", Price: "0.95", Size: "5.76",
	TxHash: "0xabc", LogIndex: 7,
}

var goodMarket = Market{CurrentPrice: 0.96, LiquidityUSD: 5000, ConditionID: "0xc", Active: true, Allowed: true}

var goodPolicy = Policy{
	Sizing: SizingFixed, FixedUSD: 25, MinSizeUSD: 5, MaxSizeUSD: 100,
	MaxSlippage: 0.03, MinLiquidityUSD: 1000,
}

func TestDecide_Propose(t *testing.T) {
	out := Decide(goodTrade, goodMarket, goodPolicy)
	if !out.Propose {
		t.Fatalf("expected propose, got skip: %s", out.Reason)
	}
	p := out.Proposal
	if p.ID != "prop-0xabc-7-0xwhale" {
		t.Errorf("ID = %q, want prop-0xabc-7-0xwhale", p.ID)
	}
	if p.Side != "buy" || p.TokenID != "tok" {
		t.Errorf("side/token = %s/%s", p.Side, p.TokenID)
	}
	if p.LimitPrice != "0.98" { // whale 0.95 + 0.03 slippage
		t.Errorf("limit = %q, want 0.98", p.LimitPrice)
	}
	if p.SizeUSD != 25 {
		t.Errorf("sizeUSD = %v, want 25", p.SizeUSD)
	}
	if p.SourceTrade.TxHash != "0xabc" {
		t.Errorf("source trade not attached")
	}
}

func TestDecide_SellLimit(t *testing.T) {
	tr := goodTrade
	tr.Side = "sell"
	tr.Price = "0.40"
	m := goodMarket
	m.CurrentPrice = 0.39
	out := Decide(tr, m, goodPolicy)
	if !out.Propose {
		t.Fatalf("expected propose, got: %s", out.Reason)
	}
	if out.Proposal.LimitPrice != "0.37" { // 0.40 - 0.03
		t.Errorf("sell limit = %q, want 0.37", out.Proposal.LimitPrice)
	}
}

func TestDecide_Skips(t *testing.T) {
	tests := []struct {
		name  string
		trade bus.TradeDetected
		mkt   Market
		pol   Policy
	}{
		{"inactive", goodTrade, mod(goodMarket, func(m *Market) { m.Active = false }), goodPolicy},
		{"not allowed", goodTrade, mod(goodMarket, func(m *Market) { m.Allowed = false }), goodPolicy},
		{"low liquidity", goodTrade, mod(goodMarket, func(m *Market) { m.LiquidityUSD = 500 }), goodPolicy},
		{"slippage", goodTrade, mod(goodMarket, func(m *Market) { m.CurrentPrice = 1.00 }), goodPolicy},
		{
			"below min size", goodTrade, goodMarket,
			Policy{Sizing: SizingProportional, Proportion: 0.01, MinSizeUSD: 5, MaxSizeUSD: 100, MaxSlippage: 0.03, MinLiquidityUSD: 1000},
		},
		{"bad price", mod2(goodTrade, func(t *bus.TradeDetected) { t.Price = "abc" }), goodMarket, goodPolicy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := Decide(tt.trade, tt.mkt, tt.pol)
			if out.Propose {
				t.Errorf("expected skip, got propose: %+v", out.Proposal)
			}
			if out.Reason == "" {
				t.Errorf("skip must have a reason")
			}
		})
	}
}

func mod(m Market, f func(*Market)) Market { f(&m); return m }
func mod2(t bus.TradeDetected, f func(*bus.TradeDetected)) bus.TradeDetected { f(&t); return t }
