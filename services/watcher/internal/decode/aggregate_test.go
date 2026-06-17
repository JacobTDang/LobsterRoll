package decode

import (
	"math/big"
	"reflect"
	"testing"
)

func fill(wallet, token string, buy bool, usdc, shares int64, tx string, idx uint64) Fill {
	return Fill{
		Wallet:      wallet,
		TokenID:     token,
		Buy:         buy,
		USDC:        big.NewInt(usdc),
		Shares:      big.NewInt(shares),
		TxHash:      tx,
		LogIndex:    idx,
		BlockNumber: 100,
	}
}

func TestAggregateByTx_VWAP(t *testing.T) {
	// Same wallet/token/side in one tx: 10 shares @0.50 + 10 shares @0.70 -> 20 @0.60.
	fills := []Fill{
		fill("0xw", "t1", true, 5_000_000, 10_000_000, "0xtx", 7),
		fill("0xw", "t1", true, 7_000_000, 10_000_000, "0xtx", 4),
	}
	got := AggregateByTx(fills)
	if len(got) != 1 {
		t.Fatalf("want 1 aggregated trade, got %d: %+v", len(got), got)
	}
	tr := got[0]
	if tr.Side != "buy" || tr.Size != "20" || tr.Price != "0.6" {
		t.Errorf("aggregated = %+v, want buy/size 20/price 0.6", tr)
	}
	if tr.LogIndex != 4 {
		t.Errorf("LogIndex = %d, want 4 (min in group)", tr.LogIndex)
	}
}

func TestAggregateByTx_SeparatesGroups(t *testing.T) {
	fills := []Fill{
		fill("0xw", "t1", true, 1_000_000, 2_000_000, "0xtx", 1),  // buy t1
		fill("0xw", "t1", false, 1_000_000, 2_000_000, "0xtx", 2), // sell t1 (diff side)
		fill("0xw", "t2", true, 1_000_000, 2_000_000, "0xtx", 3),  // buy t2 (diff token)
		fill("0xw", "t1", true, 1_000_000, 2_000_000, "0xtx2", 4), // buy t1 (diff tx)
		fill("0xv", "t1", true, 1_000_000, 2_000_000, "0xtx", 5),  // diff wallet
	}
	got := AggregateByTx(fills)
	if len(got) != 5 {
		t.Fatalf("want 5 distinct groups, got %d", len(got))
	}
}

func TestAggregateByTx_SingleFillUnchanged(t *testing.T) {
	// A lone maker fill aggregates to the same values TradeFor would produce.
	fills := []Fill{fill("0xw", "t1", false, 2_117_520, 5_190_000, "0xtx", 9)}
	got := AggregateByTx(fills)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	want := Trade{
		Wallet: "0xw", TokenID: "t1", Side: "sell",
		Price: "0.408", Size: "5.19", TxHash: "0xtx", LogIndex: 9, BlockNumber: 100,
	}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("got %+v, want %+v", got[0], want)
	}
}
