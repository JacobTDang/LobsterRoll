package format

import (
	"testing"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
)

func TestFormatAlert_Buy(t *testing.T) {
	td := bus.TradeDetected{
		Wallet: "0x037c0f46600702e77ccb738721a78d6418d3a458",
		TokenID: "25960997961246252830800085989836468476752301777787246680725159102517868182787",
		Side: "buy", Price: "0.95", Size: "5.76",
		TxHash: "0x7ccd161ea4de1234567890abcdef1234567890abcdef1234567890abcdef1234",
	}
	got := FormatAlert(td, Market{Question: "Ghana vs. Panama: O/U 2.5", Outcome: "Over", Found: true})
	want := "🟢 BUY  whale 0x037c…a458\n" +
		"Ghana vs. Panama: O/U 2.5 — Over\n" +
		"5.76 shares @ $0.95  (≈ $5.47)\n" +
		"https://polygonscan.com/tx/0x7ccd161ea4de1234567890abcdef1234567890abcdef1234567890abcdef1234"
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

func TestFormatAlert_Sell(t *testing.T) {
	td := bus.TradeDetected{
		Wallet: "0xa6d24a207011c9a5d54fa3a04f3e87365d2e12f4",
		Side:   "sell", Price: "0.408", Size: "5.19",
		TxHash: "0xdeadbeefcafebabedeadbeefcafebabedeadbeefcafebabedeadbeefcafebabe",
	}
	got := FormatAlert(td, Market{Question: "Will X happen?", Outcome: "Yes", Found: true})
	want := "🔴 SELL  whale 0xa6d2…12f4\n" +
		"Will X happen? — Yes\n" +
		"5.19 shares @ $0.408  (≈ $2.12)\n" +
		"https://polygonscan.com/tx/0xdeadbeefcafebabedeadbeefcafebabedeadbeefcafebabedeadbeefcafebabe"
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}

func TestFormatAlert_UnknownMarket(t *testing.T) {
	td := bus.TradeDetected{
		Wallet: "0x037c0f46600702e77ccb738721a78d6418d3a458",
		TokenID: "25960997961246252830800085989836468476752301777787246680725159102517868182787",
		Side: "buy", Price: "0.5", Size: "10",
		TxHash: "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
	}
	got := FormatAlert(td, Market{Found: false})
	want := "🟢 BUY  whale 0x037c…a458\n" +
		"Unknown market (token 2596…2787)\n" +
		"10 shares @ $0.5  (≈ $5.00)\n" +
		"https://polygonscan.com/tx/0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	if got != want {
		t.Fatalf("\n got: %q\nwant: %q", got, want)
	}
}
