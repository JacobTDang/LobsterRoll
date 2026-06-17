package decode

import (
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

// rawLog mirrors the fields we captured from eth_getLogs in the golden fixture.
type rawLog struct {
	Address     string   `json:"address"`
	BlockNumber string   `json:"blockNumber"`
	TxHash      string   `json:"transactionHash"`
	LogIndex    string   `json:"logIndex"`
	Topics      []string `json:"topics"`
	Data        string   `json:"data"`
}

func loadLog(t *testing.T, name string) types.Log {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "orderfilled_logs.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var m map[string]rawLog
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	r, ok := m[name]
	if !ok {
		t.Fatalf("fixture %q not found", name)
	}
	topics := make([]common.Hash, len(r.Topics))
	for i, tpc := range r.Topics {
		topics[i] = common.HexToHash(tpc)
	}
	data, err := hexutil.Decode(r.Data)
	if err != nil {
		t.Fatalf("decode data: %v", err)
	}
	blk, err := hexutil.DecodeUint64(r.BlockNumber)
	if err != nil {
		t.Fatalf("decode block: %v", err)
	}
	idx, err := hexutil.DecodeUint64(r.LogIndex)
	if err != nil {
		t.Fatalf("decode index: %v", err)
	}
	return types.Log{
		Address:     common.HexToAddress(r.Address),
		Topics:      topics,
		Data:        data,
		TxHash:      common.HexToHash(r.TxHash),
		Index:       uint(idx),
		BlockNumber: blk,
	}
}

const (
	tokenID = "100014040866852402567411479673313760539617660662946311110461989342599468569809"
	wMaker0 = "0x037c0f46600702e77ccb738721a78d6418d3a458" // maker of buy_side0
	wTaker0 = "0xa6d24a207011c9a5d54fa3a04f3e87365d2e12f4" // taker of buy_side0
)

func TestDecodeOrderFilled_Golden(t *testing.T) {
	of, err := DecodeOrderFilled(loadLog(t, "buy_side0"))
	if err != nil {
		t.Fatalf("DecodeOrderFilled: %v", err)
	}
	if of.Side != 0 {
		t.Errorf("Side = %d, want 0 (BUY)", of.Side)
	}
	if of.TokenID.String() != tokenID {
		t.Errorf("TokenID = %s, want %s", of.TokenID, tokenID)
	}
	if of.Maker.Hex() != common.HexToAddress(wMaker0).Hex() {
		t.Errorf("Maker = %s, want %s", of.Maker.Hex(), wMaker0)
	}
	if of.Taker.Hex() != common.HexToAddress(wTaker0).Hex() {
		t.Errorf("Taker = %s, want %s", of.Taker.Hex(), wTaker0)
	}
	if of.MakerAmount.Cmp(big.NewInt(5472000)) != 0 {
		t.Errorf("MakerAmount = %s, want 5472000", of.MakerAmount)
	}
	if of.TakerAmount.Cmp(big.NewInt(5760000)) != 0 {
		t.Errorf("TakerAmount = %s, want 5760000", of.TakerAmount)
	}
	if of.Fee.Sign() != 0 {
		t.Errorf("Fee = %s, want 0", of.Fee)
	}
}

func TestTradeFor_Perspective(t *testing.T) {
	of, err := DecodeOrderFilled(loadLog(t, "buy_side0"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Maker's perspective: maker side is BUY.
	tr, ok := of.TradeFor(common.HexToAddress(wMaker0))
	if !ok {
		t.Fatal("maker should match")
	}
	if tr.Side != "buy" {
		t.Errorf("maker side = %q, want buy", tr.Side)
	}
	if tr.Price != "0.95" {
		t.Errorf("price = %q, want 0.95", tr.Price)
	}
	if tr.Size != "5.76" {
		t.Errorf("size = %q, want 5.76", tr.Size)
	}
	if tr.Wallet != wMaker0 {
		t.Errorf("wallet = %q, want %q", tr.Wallet, wMaker0)
	}
	if tr.TokenID != tokenID {
		t.Errorf("tokenID = %q", tr.TokenID)
	}
	if tr.LogIndex != 185 || tr.BlockNumber != 88641258 {
		t.Errorf("logIndex/block = %d/%d, want 185/88641258", tr.LogIndex, tr.BlockNumber)
	}

	// Taker's perspective on the same fill: side is inverted (SELL), same price/size.
	tr2, ok := of.TradeFor(common.HexToAddress(wTaker0))
	if !ok {
		t.Fatal("taker should match")
	}
	if tr2.Side != "sell" {
		t.Errorf("taker side = %q, want sell (inverted)", tr2.Side)
	}
	if tr2.Price != "0.95" || tr2.Size != "5.76" {
		t.Errorf("taker price/size = %q/%q, want 0.95/5.76", tr2.Price, tr2.Size)
	}
	if tr2.Wallet != wTaker0 {
		t.Errorf("taker wallet = %q, want %q", tr2.Wallet, wTaker0)
	}

	// An unrelated wallet does not match.
	if _, ok := of.TradeFor(common.HexToAddress("0x000000000000000000000000000000000000dead")); ok {
		t.Error("unrelated wallet should not match")
	}
}

func TestTradeFor_SellSide(t *testing.T) {
	of, err := DecodeOrderFilled(loadLog(t, "sell_side1"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if of.Side != 1 {
		t.Fatalf("Side = %d, want 1 (SELL)", of.Side)
	}
	// Maker sold: SELL at the same 0.95 / 5.76.
	tr, ok := of.TradeFor(of.Maker)
	if !ok {
		t.Fatal("maker should match")
	}
	if tr.Side != "sell" || tr.Price != "0.95" || tr.Size != "5.76" {
		t.Errorf("sell maker trade = %+v, want sell/0.95/5.76", tr)
	}
}

func TestDecodeOrderFilled_Rejects(t *testing.T) {
	good := loadLog(t, "buy_side0")

	// Wrong topic0.
	bad := good
	bad.Topics = append([]common.Hash{}, good.Topics...)
	bad.Topics[0] = common.HexToHash("0xdeadbeef")
	if _, err := DecodeOrderFilled(bad); err == nil {
		t.Error("expected error on wrong topic0")
	}

	// Too few topics.
	bad2 := good
	bad2.Topics = good.Topics[:3]
	if _, err := DecodeOrderFilled(bad2); err == nil {
		t.Error("expected error on too few topics")
	}

	// Truncated data.
	bad3 := good
	bad3.Data = good.Data[:len(good.Data)-1]
	if _, err := DecodeOrderFilled(bad3); err == nil {
		t.Error("expected error on truncated data")
	}
}
