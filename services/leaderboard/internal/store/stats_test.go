package store

import (
	"context"
	"testing"
)

func TestStats_UpsertAndGet(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)

	if _, found, err := s.GetStats(ctx, "0xabc"); err != nil || found {
		t.Fatalf("GetStats absent = (found=%v, err=%v), want (false, nil)", found, err)
	}

	rec := StatsRecord{
		Wallet: "0xabc", WinRate: 0.65, ResolvedMarkets: 29, RealizedPnL: 31_000_000,
		ROI: 0.42, Fresh: true, Profit30D: 1_234.5, PortfolioValue: 999.9, TradedMarkets: 40, ComputedUnix: 1700000000,
	}
	if err := s.UpsertStats(ctx, rec); err != nil {
		t.Fatalf("UpsertStats: %v", err)
	}

	got, found, err := s.GetStats(ctx, "0xabc")
	if err != nil || !found {
		t.Fatalf("GetStats = (found=%v, err=%v), want (true, nil)", found, err)
	}
	if got != rec {
		t.Fatalf("GetStats = %+v, want %+v", got, rec)
	}
}

func TestStats_UpsertPreservesCLV(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	rec := StatsRecord{Wallet: "0xabc", WinRate: 0.6, ResolvedMarkets: 25, ComputedUnix: 1700000000}
	if err := s.UpsertStats(ctx, rec); err != nil {
		t.Fatalf("UpsertStats: %v", err)
	}
	if err := s.SetWalletCLV(ctx, "0xabc", 0.08, 100); err != nil {
		t.Fatalf("SetWalletCLV: %v", err)
	}
	// A stats refresh must NOT clobber CLV — zeroing it mid-refresh would serve
	// avg_clv=0 to the trader for the whole crawl. CLV is owned by SetWalletCLV.
	if err := s.UpsertStats(ctx, rec); err != nil {
		t.Fatalf("UpsertStats #2: %v", err)
	}
	if got, _, _ := s.GetStats(ctx, "0xabc"); got.AvgCLV != 0.08 || got.CLVN != 100 {
		t.Fatalf("after refresh: avgCLV=%v n=%v, want 0.08/100 (CLV must survive a refresh)", got.AvgCLV, got.CLVN)
	}
	// Stale CLV is cleared explicitly (this is what enrichCLV calls for dropped wallets).
	if err := s.SetWalletCLV(ctx, "0xabc", 0, 0); err != nil {
		t.Fatalf("SetWalletCLV clear: %v", err)
	}
	if got, _, _ := s.GetStats(ctx, "0xabc"); got.AvgCLV != 0 || got.CLVN != 0 {
		t.Fatalf("after clear: avgCLV=%v n=%v, want 0/0", got.AvgCLV, got.CLVN)
	}
}

func TestStats_UpsertReplaces(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)

	base := StatsRecord{Wallet: "0xabc", WinRate: 0.5, ResolvedMarkets: 10, RealizedPnL: 100, ComputedUnix: 1}
	if err := s.UpsertStats(ctx, base); err != nil {
		t.Fatalf("UpsertStats: %v", err)
	}
	updated := StatsRecord{Wallet: "0xabc", WinRate: 0.8, ResolvedMarkets: 20, RealizedPnL: 500, ComputedUnix: 2}
	if err := s.UpsertStats(ctx, updated); err != nil {
		t.Fatalf("UpsertStats update: %v", err)
	}
	got, _, err := s.GetStats(ctx, "0xabc")
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if got != updated {
		t.Fatalf("GetStats = %+v, want %+v (upsert must replace)", got, updated)
	}
}

// Stats methods must coexist with the existing watchset schema.
func TestStats_CoexistsWithWatchset(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)

	if _, err := s.Replace(ctx, []string{"0xa", "0xb"}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if err := s.UpsertStats(ctx, StatsRecord{Wallet: "0xa", ComputedUnix: 1}); err != nil {
		t.Fatalf("UpsertStats: %v", err)
	}
	ws, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ws) != 2 {
		t.Fatalf("watchset = %v, want 2 entries", ws)
	}
	if _, found, _ := s.GetStats(ctx, "0xa"); !found {
		t.Fatal("stats for 0xa not found")
	}
}
