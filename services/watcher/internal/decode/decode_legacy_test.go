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

func loadLogFile(t *testing.T, file, name string) types.Log {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", file))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var m map[string]rawLog
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	r := m[name]
	topics := make([]common.Hash, len(r.Topics))
	for i, tpc := range r.Topics {
		topics[i] = common.HexToHash(tpc)
	}
	data, _ := hexutil.Decode(r.Data)
	blk, _ := hexutil.DecodeUint64(r.BlockNumber)
	idx, _ := hexutil.DecodeUint64(r.LogIndex)
	return types.Log{
		Address:     common.HexToAddress(r.Address),
		Topics:      topics,
		Data:        data,
		TxHash:      common.HexToHash(r.TxHash),
		Index:       uint(idx),
		BlockNumber: blk,
	}
}

const legacyTokenID = "94465431535186377718946886116016148494509197935308351650618834567946692622519"

func TestDecodeLegacy_Golden(t *testing.T) {
	log := loadLogFile(t, "orderfilled_legacy.json", "legacy")
	of, err := DecodeOrderFilledLegacy(log)
	if err != nil {
		t.Fatalf("DecodeOrderFilledLegacy: %v", err)
	}
	// makerAssetId != 0, takerAssetId == 0 => maker is SELLING the token.
	if of.Side != sideSell {
		t.Errorf("Side = %d, want 1 (SELL)", of.Side)
	}
	if of.TokenID.String() != legacyTokenID {
		t.Errorf("TokenID = %s, want %s", of.TokenID, legacyTokenID)
	}
	if of.MakerAmount.Cmp(big.NewInt(5190000)) != 0 {
		t.Errorf("MakerAmount = %s, want 5190000", of.MakerAmount)
	}
	if of.TakerAmount.Cmp(big.NewInt(2117520)) != 0 {
		t.Errorf("TakerAmount = %s, want 2117520", of.TakerAmount)
	}

	// Maker's perspective: SELL 5.19 shares at 0.408.
	tr, ok := of.TradeFor(of.Maker)
	if !ok {
		t.Fatal("maker should match")
	}
	if tr.Side != "sell" || tr.Price != "0.408" || tr.Size != "5.19" {
		t.Errorf("legacy trade = %+v, want sell/0.408/5.19", tr)
	}
}

func TestDecode_DispatchesByTopic(t *testing.T) {
	// Current-ABI log routes to the new decoder.
	cur := loadLogFile(t, "orderfilled_logs.json", "buy_side0")
	of, err := Decode(cur)
	if err != nil {
		t.Fatalf("Decode(current): %v", err)
	}
	if of.Side != sideBuy {
		t.Errorf("current Side = %d, want 0", of.Side)
	}

	// Legacy-ABI log routes to the legacy decoder.
	leg := loadLogFile(t, "orderfilled_legacy.json", "legacy")
	of2, err := Decode(leg)
	if err != nil {
		t.Fatalf("Decode(legacy): %v", err)
	}
	if of2.TokenID.String() != legacyTokenID {
		t.Errorf("legacy TokenID = %s", of2.TokenID)
	}

	// Unknown topic0 is rejected.
	bad := cur
	bad.Topics = append([]common.Hash{}, cur.Topics...)
	bad.Topics[0] = common.HexToHash("0xabc")
	if _, err := Decode(bad); err == nil {
		t.Error("expected error for unknown topic0")
	}
}
