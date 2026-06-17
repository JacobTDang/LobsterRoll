//go:build integration

package client

import (
	"context"
	"testing"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/chain"
)

// TestFetch_Live hits the real Polymarket lb-api. Run with:
//
//	go test -tags=integration ./services/leaderboard/...
func TestFetch_Live(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	wallets, err := New(DefaultBaseURL, nil).Fetch(ctx, MetricPNL, "7d", 5)
	if err != nil {
		t.Fatalf("live Fetch: %v", err)
	}
	if len(wallets) == 0 {
		t.Fatal("live leaderboard returned no wallets")
	}
	for _, w := range wallets {
		if _, ok := chain.NormalizeAddress(w); !ok {
			t.Errorf("non-normalized wallet from live API: %q", w)
		}
	}
	t.Logf("live top-%d pnl wallets: %v", len(wallets), wallets)
}
