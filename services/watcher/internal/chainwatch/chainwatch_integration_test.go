//go:build integration

package chainwatch

import (
	"context"
	"os"
	"testing"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/JacobTDang/LobsterRoll/pkg/chain"
	"github.com/JacobTDang/LobsterRoll/services/watcher/internal/decode"
)

// TestSubscribe_LiveDecode connects to a real Polygon WSS endpoint and decodes
// at least one live OrderFilled within a timeout. Run with:
//
//	RPC_WSS_URL=wss://... go test -tags=integration ./services/watcher/...
func TestSubscribe_LiveDecode(t *testing.T) {
	url := os.Getenv("RPC_WSS_URL")
	if url == "" {
		t.Skip("RPC_WSS_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	ec, err := ethclient.DialContext(ctx, url)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer ec.Close()

	addrs := make([]common.Address, 0)
	for _, a := range chain.WatchedExchanges() {
		addrs = append(addrs, common.HexToAddress(a))
	}
	q := ethereum.FilterQuery{
		Addresses: addrs,
		Topics: [][]common.Hash{{
			common.HexToHash(chain.OrderFilledTopic),
			common.HexToHash(chain.OrderFilledTopicLegacy),
		}},
	}
	ch := make(chan types.Log, 16)
	sub, err := ec.SubscribeFilterLogs(ctx, q, ch)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Unsubscribe()

	select {
	case err := <-sub.Err():
		t.Fatalf("subscription error: %v", err)
	case l := <-ch:
		of, err := decode.Decode(l)
		if err != nil {
			t.Fatalf("decode live log: %v", err)
		}
		if of.TokenID == nil || of.TokenID.Sign() == 0 {
			t.Errorf("decoded tokenId looks wrong: %v", of.TokenID)
		}
		t.Logf("decoded live OrderFilled: tx=%s side=%d token=%s maker=%s taker=%s",
			of.TxHash.Hex(), of.Side, of.TokenID, of.Maker.Hex(), of.Taker.Hex())
	case <-ctx.Done():
		t.Fatal("timed out waiting for a live OrderFilled")
	}
}
