package format

import (
	"strings"
	"testing"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
)

func TestFormatProposal(t *testing.T) {
	p := bus.OrderProposal{
		ID: "prop-1", TokenID: "25960997961246252830800085989836468476752301777787246680725159102517868182787",
		Side: "buy", LimitPrice: "0.98", SizeUSD: 25,
		SourceTrade: bus.TradeDetected{Wallet: "0x037c0f46600702e77ccb738721a78d6418d3a458", Size: "5.76", Price: "0.95"},
	}
	got := FormatProposal(p)
	want := "📋 Mirror BUY  $25.00 @ ≤ $0.98\n" +
		"token 2596…2787\n" +
		"whale 0x037c…a458 filled 5.76 @ $0.95\n" +
		"Approve?"
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

func TestFormatAlert_BuyEnter(t *testing.T) {
	td := bus.TradeDetected{
		Wallet:  "0x037c0f46600702e77ccb738721a78d6418d3a458",
		TokenID: "25960997961246252830800085989836468476752301777787246680725159102517868182787",
		Side:    "buy", Price: "0.95", Size: "5.76",
		TxHash:     "0x7ccd161ea4de1234567890abcdef1234567890abcdef1234567890abcdef1234",
		ObservedAt: time.Date(2026, 6, 17, 12, 5, 0, 0, time.UTC),
	}
	got := FormatAlert(td, Market{Question: "Ghana vs. Panama: O/U 2.5", Outcome: "Over", Slug: "fifwc-gha-pan-total-2pt5", Found: true}, WhaleStats{})
	want := "🟢 ENTER (BUY)  whale 0x037c…a458\n" +
		"Ghana vs. Panama: O/U 2.5 → Over\n" +
		"💵 $5.47  ·  5.76 @ $0.95\n" +
		"🕒 2026-06-17 12:05 UTC\n" +
		"📊 https://polymarket.com/event/fifwc-gha-pan-total-2pt5"
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

func TestFormatAlert_WithWhaleStats(t *testing.T) {
	td := bus.TradeDetected{
		Wallet:  "0x037c0f46600702e77ccb738721a78d6418d3a458",
		TokenID: "25960997961246252830800085989836468476752301777787246680725159102517868182787",
		Side:    "buy", Price: "0.95", Size: "5.76",
		TxHash:     "0x7ccd161ea4de1234567890abcdef1234567890abcdef1234567890abcdef1234",
		ObservedAt: time.Date(2026, 6, 17, 12, 5, 0, 0, time.UTC),
	}
	stats := WhaleStats{WinRate: 0.65, ResolvedMarkets: 29, RealizedPnlUSD: 31_000_000, PortfolioUSD: 1200, OK: true}
	got := FormatAlert(td, Market{Question: "Ghana vs. Panama: O/U 2.5", Outcome: "Over", Slug: "fifwc-gha-pan-total-2pt5", Found: true}, stats)
	want := "🟢 ENTER (BUY)  whale 0x037c…a458\n" +
		"Ghana vs. Panama: O/U 2.5 → Over\n" +
		"👤 65% win (29 mkts) · realized +$31.0M · $1.2k portfolio\n" +
		"💵 $5.47  ·  5.76 @ $0.95\n" +
		"🕒 2026-06-17 12:05 UTC\n" +
		"📊 https://polymarket.com/event/fifwc-gha-pan-total-2pt5"
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

func TestFormatAlert_SellExit(t *testing.T) {
	// No ObservedAt and no slug → those lines are omitted.
	td := bus.TradeDetected{
		Wallet: "0xa6d24a207011c9a5d54fa3a04f3e87365d2e12f4",
		Side:   "sell", Price: "0.408", Size: "5.19",
		TxHash: "0xdeadbeefcafebabedeadbeefcafebabedeadbeefcafebabedeadbeefcafebabe",
	}
	got := FormatAlert(td, Market{Question: "Will X happen?", Outcome: "Yes", Found: true}, WhaleStats{})
	want := "🔴 EXIT (SELL)  whale 0xa6d2…12f4\n" +
		"Will X happen? → Yes\n" +
		"💵 $2.12  ·  5.19 @ $0.408"
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

func TestFormatAlert_ShowsGameEndDate(t *testing.T) {
	td := bus.TradeDetected{Wallet: "0x037c0f46600702e77ccb738721a78d6418d3a458", Side: "buy", Price: "0.5", Size: "10"}
	end := time.Date(2026, 6, 27, 21, 0, 0, 0, time.UTC).Unix()
	got := FormatAlert(td, Market{Question: "Q", Outcome: "Yes", Found: true, EndDateUnix: end}, WhaleStats{})
	if !strings.Contains(got, "🏁 game 2026-06-27 21:00 UTC") {
		t.Errorf("missing game end date line: %q", got)
	}
}

func TestFormatAlert_StatsOmittedWhenNotOK(t *testing.T) {
	td := bus.TradeDetected{
		Wallet: "0xa6d24a207011c9a5d54fa3a04f3e87365d2e12f4",
		Side:   "sell", Price: "0.408", Size: "5.19",
		TxHash: "0xdeadbeefcafebabedeadbeefcafebabedeadbeefcafebabedeadbeefcafebabe",
	}
	// Stats present but OK=false → line omitted entirely.
	got := FormatAlert(td, Market{Question: "Will X happen?", Outcome: "Yes", Found: true},
		WhaleStats{WinRate: 0.9, ResolvedMarkets: 5, RealizedPnlUSD: 100, PortfolioUSD: 50})
	if strings.Contains(got, "👤") {
		t.Fatalf("stats line should be omitted when !OK: %q", got)
	}
}

func TestFormatAlert_LookupUnavailable(t *testing.T) {
	td := bus.TradeDetected{
		Wallet:  "0x037c0f46600702e77ccb738721a78d6418d3a458",
		TokenID: "25960997961246252830800085989836468476752301777787246680725159102517868182787",
		Side:    "buy", Price: "0.5", Size: "10",
		TxHash: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
	}
	got := FormatAlert(td, Market{LookupFailed: true}, WhaleStats{})
	if !strings.Contains(got, "Market lookup unavailable (token 2596…2787)") {
		t.Fatalf("got %q", got)
	}
}

func TestFormatAlert_UnknownMarket(t *testing.T) {
	td := bus.TradeDetected{
		Wallet:  "0x037c0f46600702e77ccb738721a78d6418d3a458",
		TokenID: "25960997961246252830800085989836468476752301777787246680725159102517868182787",
		Side:    "buy", Price: "0.5", Size: "10",
		TxHash: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
	}
	got := FormatAlert(td, Market{Found: false}, WhaleStats{})
	want := "🟢 ENTER (BUY)  whale 0x037c…a458\n" +
		"Unknown market (token 2596…2787)\n" +
		"💵 $5.00  ·  10 @ $0.5"
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

func TestFormatConsensus_Found(t *testing.T) {
	sig := bus.ConsensusSignal{
		TokenID:     "25960997961246252830800085989836468476752301777787246680725159102517868182787",
		Side:        "buy",
		Wallets:     []string{"0xaaa", "0xbbb", "0xccc", "0xddd"},
		Count:       4,
		CombinedUSD: 12000,
		WindowSecs:  6 * 3600,
		ObservedAt:  time.Date(2026, 6, 17, 12, 5, 0, 0, time.UTC),
	}
	got := FormatConsensus(sig, Market{Question: "Ghana vs. Panama: O/U 2.5", Outcome: "Over", Slug: "fifwc-gha-pan-total-2pt5", Found: true})
	want := "🔥 CONSENSUS — 4 tracked wallets BUY\n" +
		"Ghana vs. Panama: O/U 2.5 → Over\n" +
		"4 wallets · combined $12.0k · 6h window\n" +
		"📊 https://polymarket.com/event/fifwc-gha-pan-total-2pt5"
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

func TestFormatConsensus_Sell(t *testing.T) {
	sig := bus.ConsensusSignal{
		TokenID: "2596", Side: "sell", Count: 3, CombinedUSD: 450, WindowSecs: 1800,
	}
	got := FormatConsensus(sig, Market{Found: false})
	want := "🔥 CONSENSUS — 3 tracked wallets SELL\n" +
		"Unknown market (token 2596)\n" +
		"3 wallets · combined $450 · 30m window"
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

func TestFormatConsensus_LookupUnavailable(t *testing.T) {
	sig := bus.ConsensusSignal{
		TokenID: "25960997961246252830800085989836468476752301777787246680725159102517868182787",
		Side:    "buy", Count: 2, CombinedUSD: 5000, WindowSecs: 3600,
	}
	got := FormatConsensus(sig, Market{LookupFailed: true})
	if !strings.Contains(got, "Market lookup unavailable (token 2596…2787)") {
		t.Fatalf("got %q", got)
	}
}

func TestAbbrevMoney(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "$0"},
		{45, "$45"},
		{-45, "$45"},
		{1200, "$1.2k"},
		{12345, "$12.3k"},
		{999, "$999"},
		{1_000_000, "$1.0M"},
		{31_000_000, "$31.0M"},
		{2_500_000, "$2.5M"},
	}
	for _, tt := range tests {
		if got := abbrevMoney(tt.in); got != tt.want {
			t.Errorf("abbrevMoney(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSignedMoney(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0, "$0"},
		{31_000_000, "+$31.0M"},
		{12345, "+$12.3k"},
		{-45, "-$45"},
		{-2_500_000, "-$2.5M"},
	}
	for _, tt := range tests {
		if got := signedMoney(tt.in); got != tt.want {
			t.Errorf("signedMoney(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestHumanWindow(t *testing.T) {
	tests := []struct {
		secs int
		want string
	}{
		{1800, "30m"},
		{3600, "1h"},
		{6 * 3600, "6h"},
		{90, "1m"},
		{0, "0m"},
		{45, "0m"},
	}
	for _, tt := range tests {
		if got := humanWindow(tt.secs); got != tt.want {
			t.Errorf("humanWindow(%d) = %q, want %q", tt.secs, got, tt.want)
		}
	}
}
