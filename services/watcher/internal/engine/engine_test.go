package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/dedup"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/watchset"
)

type rawLog struct {
	Address     string   `json:"address"`
	BlockNumber string   `json:"blockNumber"`
	TxHash      string   `json:"transactionHash"`
	LogIndex    string   `json:"logIndex"`
	Topics      []string `json:"topics"`
	Data        string   `json:"data"`
}

func loadLogs(t *testing.T) []types.Log {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "orderfilled_logs.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var m map[string]rawLog
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var out []types.Log
	for _, name := range []string{"buy_side0", "sell_side1"} {
		r := m[name]
		topics := make([]common.Hash, len(r.Topics))
		for i, tpc := range r.Topics {
			topics[i] = common.HexToHash(tpc)
		}
		data, _ := hexutil.Decode(r.Data)
		blk, _ := hexutil.DecodeUint64(r.BlockNumber)
		idx, _ := hexutil.DecodeUint64(r.LogIndex)
		out = append(out, types.Log{
			Address: common.HexToAddress(r.Address), Topics: topics, Data: data,
			TxHash: common.HexToHash(r.TxHash), Index: uint(idx), BlockNumber: blk,
		})
	}
	return out
}

// aggressor is the taker of buy_side0 and the maker of sell_side1 (same tx):
// watching it must aggregate BOTH logs into one SELL.
const aggressor = "0xa6d24a207011c9a5d54fa3a04f3e87365d2e12f4"

func TestProcessBatch_AggregatesAcrossLogsInTx(t *testing.T) {
	logs := loadLogs(t)
	set := watchset.New()
	set.Apply([]string{aggressor}, nil)
	seen := dedup.New()

	trades, maxBlock := ProcessBatch(logs, set, seen, nil)
	if len(trades) != 1 {
		t.Fatalf("want 1 aggregated trade, got %d: %+v", len(trades), trades)
	}
	tr := trades[0]
	if tr.Wallet != aggressor {
		t.Errorf("wallet = %q, want %q", tr.Wallet, aggressor)
	}
	if tr.Side != "sell" {
		t.Errorf("side = %q, want sell", tr.Side)
	}
	// 5.76 + 5.76 shares; 5.472 + 5.472 USDC -> price 0.95.
	if tr.Size != "11.52" {
		t.Errorf("size = %q, want 11.52", tr.Size)
	}
	if tr.Price != "0.95" {
		t.Errorf("price = %q, want 0.95", tr.Price)
	}
	if maxBlock != 88641258 {
		t.Errorf("maxBlock = %d, want 88641258", maxBlock)
	}
}

func TestProcessBatch_DedupAcrossBatches(t *testing.T) {
	logs := loadLogs(t)
	set := watchset.New()
	set.Apply([]string{aggressor}, nil)
	seen := dedup.New()

	if trades, _ := ProcessBatch(logs, set, seen, nil); len(trades) != 1 {
		t.Fatalf("first batch: want 1, got %d", len(trades))
	}
	// Same logs again (backfill/live overlap) -> nothing new.
	if trades, _ := ProcessBatch(logs, set, seen, nil); len(trades) != 0 {
		t.Fatalf("second batch: want 0 (deduped), got %d", len(trades))
	}
}

func TestProcessBatch_IgnoresUnwatched(t *testing.T) {
	logs := loadLogs(t)
	set := watchset.New() // nobody watched
	seen := dedup.New()

	trades, _ := ProcessBatch(logs, set, seen, nil)
	if len(trades) != 0 {
		t.Fatalf("want 0 trades for empty watchset, got %d", len(trades))
	}
	// Unwatched logs must NOT consume dedup slots.
	if seen.Len() != 0 {
		t.Errorf("seen.Len = %d, want 0 (unwatched logs not recorded)", seen.Len())
	}
}

func TestProcessBatch_SkipsUndecodable(t *testing.T) {
	logs := loadLogs(t)
	// Corrupt the first log's topic0 so it can't decode.
	logs[0].Topics = append([]common.Hash{}, logs[0].Topics...)
	logs[0].Topics[0] = common.HexToHash("0xdeadbeef")

	set := watchset.New()
	set.Apply([]string{aggressor}, nil)
	seen := dedup.New()

	// Should not panic; the still-valid sell_side1 (maker=aggressor) yields a trade.
	trades, _ := ProcessBatch(logs, set, seen, nil)
	if len(trades) != 1 {
		t.Fatalf("want 1 trade from the valid log, got %d", len(trades))
	}
	if trades[0].Size != "5.76" {
		t.Errorf("size = %q, want 5.76 (only the decodable log)", trades[0].Size)
	}
}
