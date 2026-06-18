package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/dedup"
)

type fakeEnricher struct {
	resp *lobsterrollv1.EnrichTokenResponse
	err  error
}

func (f fakeEnricher) EnrichToken(context.Context, *lobsterrollv1.EnrichTokenRequest, ...grpc.CallOption) (*lobsterrollv1.EnrichTokenResponse, error) {
	return f.resp, f.err
}

type fakeStats struct {
	resp   *lobsterrollv1.WalletStats
	err    error
	calls  int
	wallet string
}

func (f *fakeStats) GetWalletStats(_ context.Context, in *lobsterrollv1.GetWalletStatsRequest, _ ...grpc.CallOption) (*lobsterrollv1.WalletStats, error) {
	f.calls++
	f.wallet = in.GetWallet()
	return f.resp, f.err
}

type fakeSender struct {
	chatID string
	text   string
	calls  int
	err    error
}

func (s *fakeSender) Send(_ context.Context, chatID, text string) error {
	s.calls++
	s.chatID, s.text = chatID, text
	return s.err
}

func dd() *dedup.TTLSet { return dedup.New(time.Hour) }

// cd is a cooldown set for tests; nil-equivalent behavior is tested separately.
func cd() *dedup.TTLSet { return dedup.New(time.Hour) }

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

var trade = bus.TradeDetected{
	Wallet:  "0x037c0f46600702e77ccb738721a78d6418d3a458",
	TokenID: "2596", Side: "buy", Price: "0.95", Size: "5.76",
	TxHash: "0x7ccd161ea4de1234567890abcdef1234567890abcdef1234567890abcdef1234",
}

func TestHandle_Enriched(t *testing.T) {
	enr := fakeEnricher{resp: &lobsterrollv1.EnrichTokenResponse{MarketQuestion: "Ghana vs. Panama: O/U 2.5", Outcome: "Over"}}
	snd := &fakeSender{}
	New(enr, nil, snd, "999", dd(), cd(), quiet()).Handle(context.Background(), trade)

	if snd.calls != 1 {
		t.Fatalf("send calls = %d, want 1", snd.calls)
	}
	if snd.chatID != "999" {
		t.Errorf("chatID = %q, want 999", snd.chatID)
	}
	if !strings.Contains(snd.text, "Ghana vs. Panama: O/U 2.5 → Over") {
		t.Errorf("text missing market: %q", snd.text)
	}
	if !strings.Contains(snd.text, "🟢 ENTER (BUY)") {
		t.Errorf("text missing buy marker: %q", snd.text)
	}
}

func TestHandle_DeduplicatesRepeatTrade(t *testing.T) {
	enr := fakeEnricher{resp: &lobsterrollv1.EnrichTokenResponse{MarketQuestion: "Q", Outcome: "Yes"}}
	snd := &fakeSender{}
	h := New(enr, nil, snd, "1", dd(), cd(), quiet())

	h.Handle(context.Background(), trade)
	h.Handle(context.Background(), trade) // same trade re-emitted by the watcher
	if snd.calls != 1 {
		t.Fatalf("send calls = %d, want 1 (duplicate suppressed)", snd.calls)
	}
	// A genuinely different leg (other side) of the same tx still alerts.
	other := trade
	other.Side = "sell"
	other.LogIndex = trade.LogIndex + 1
	h.Handle(context.Background(), other)
	if snd.calls != 2 {
		t.Fatalf("send calls = %d, want 2 (distinct leg must alert)", snd.calls)
	}
}

func TestHandle_CooldownCollapsesScalingIn(t *testing.T) {
	enr := fakeEnricher{resp: &lobsterrollv1.EnrichTokenResponse{MarketQuestion: "Q", Outcome: "Yes"}}
	snd := &fakeSender{}
	h := New(enr, nil, snd, "1", dd(), cd(), quiet())

	// Whale scales into the same market+side via two SEPARATE txs (distinct fills,
	// so the exact-dedup lets both through) — the cooldown must collapse to one.
	t1 := bus.TradeDetected{Wallet: "0xWHALE", TokenID: "789", Side: "buy", Price: "0.5", Size: "5", TxHash: "0xaaa", LogIndex: 1}
	t2 := bus.TradeDetected{Wallet: "0xWHALE", TokenID: "789", Side: "buy", Price: "0.5", Size: "5", TxHash: "0xbbb", LogIndex: 2}
	h.Handle(context.Background(), t1)
	h.Handle(context.Background(), t2)
	if snd.calls != 1 {
		t.Fatalf("send calls = %d, want 1 (scaling-in collapsed by cooldown)", snd.calls)
	}

	// The opposite side (an exit) is NOT cooled down — different side key.
	sell := bus.TradeDetected{Wallet: "0xWHALE", TokenID: "789", Side: "sell", Price: "0.6", Size: "5", TxHash: "0xccc", LogIndex: 3}
	h.Handle(context.Background(), sell)
	if snd.calls != 2 {
		t.Fatalf("send calls = %d, want 2 (opposite side must still alert)", snd.calls)
	}
}

func TestHandle_SkipsPastGames(t *testing.T) {
	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	past := now.Add(-2 * time.Hour).Unix()
	future := now.Add(2 * time.Hour).Unix()

	mk := func(end int64) *fakeSender {
		enr := fakeEnricher{resp: &lobsterrollv1.EnrichTokenResponse{
			MarketQuestion: "Q", Outcome: "Yes", EndDateUnix: end,
		}}
		snd := &fakeSender{}
		h := New(enr, nil, snd, "1", dd(), cd(), quiet())
		h.now = func() time.Time { return now }
		h.Handle(context.Background(), trade)
		return snd
	}

	if c := mk(past).calls; c != 0 {
		t.Fatalf("past game: send calls = %d, want 0 (already over)", c)
	}
	if c := mk(future).calls; c != 1 {
		t.Fatalf("future game: send calls = %d, want 1", c)
	}
	if c := mk(0).calls; c != 1 {
		t.Fatalf("unknown end date: send calls = %d, want 1 (don't filter)", c)
	}
}

func TestHandle_NilCooldownDisablesCollapsing(t *testing.T) {
	enr := fakeEnricher{resp: &lobsterrollv1.EnrichTokenResponse{MarketQuestion: "Q", Outcome: "Yes"}}
	snd := &fakeSender{}
	h := New(enr, nil, snd, "1", dd(), nil, quiet()) // cooldown disabled

	t1 := bus.TradeDetected{Wallet: "0xW", TokenID: "1", Side: "buy", Size: "5", TxHash: "0xa", LogIndex: 1}
	t2 := bus.TradeDetected{Wallet: "0xW", TokenID: "1", Side: "buy", Size: "5", TxHash: "0xb", LogIndex: 2}
	h.Handle(context.Background(), t1)
	h.Handle(context.Background(), t2)
	if snd.calls != 2 {
		t.Fatalf("send calls = %d, want 2 (nil cooldown = no collapsing)", snd.calls)
	}
}

func TestHandle_SendFailureNotDeduped(t *testing.T) {
	enr := fakeEnricher{resp: &lobsterrollv1.EnrichTokenResponse{MarketQuestion: "Q", Outcome: "Yes"}}
	snd := &fakeSender{err: errors.New("telegram down")}
	h := New(enr, nil, snd, "1", dd(), cd(), quiet())

	h.Handle(context.Background(), trade) // fails to send -> must NOT be cached
	snd.err = nil
	h.Handle(context.Background(), trade) // redelivery should now succeed
	if snd.calls != 2 {
		t.Fatalf("send attempts = %d, want 2 (failed send must be retryable)", snd.calls)
	}
}

func TestHandle_WithStats_RendersStatsLine(t *testing.T) {
	enr := fakeEnricher{resp: &lobsterrollv1.EnrichTokenResponse{MarketQuestion: "Q", Outcome: "Yes"}}
	st := &fakeStats{resp: &lobsterrollv1.WalletStats{
		WinRate: 0.65, ResolvedMarkets: 29, RealizedPnl: 31_000_000, PortfolioValue: 1200, Found: true,
	}}
	snd := &fakeSender{}
	New(enr, st, snd, "1", dd(), cd(), quiet()).Handle(context.Background(), trade)

	if st.calls != 1 {
		t.Fatalf("stats lookups = %d, want 1", st.calls)
	}
	if st.wallet != trade.Wallet {
		t.Errorf("stats wallet = %q, want %q", st.wallet, trade.Wallet)
	}
	if !strings.Contains(snd.text, "👤 65% win (29 mkts) · realized +$31.0M · $1.2k portfolio") {
		t.Errorf("text missing stats line: %q", snd.text)
	}
}

func TestHandle_StatsNotFound_OmitsLine(t *testing.T) {
	enr := fakeEnricher{resp: &lobsterrollv1.EnrichTokenResponse{MarketQuestion: "Q", Outcome: "Yes"}}
	st := &fakeStats{resp: &lobsterrollv1.WalletStats{Found: false}}
	snd := &fakeSender{}
	New(enr, st, snd, "1", dd(), cd(), quiet()).Handle(context.Background(), trade)

	if snd.calls != 1 {
		t.Fatalf("send calls = %d, want 1", snd.calls)
	}
	if strings.Contains(snd.text, "👤") {
		t.Errorf("stats line should be omitted when !Found: %q", snd.text)
	}
}

func TestHandle_StatsError_OmitsLine_StillAlerts(t *testing.T) {
	enr := fakeEnricher{resp: &lobsterrollv1.EnrichTokenResponse{MarketQuestion: "Q", Outcome: "Yes"}}
	st := &fakeStats{err: status.Error(codes.Unavailable, "leaderboard down")}
	snd := &fakeSender{}
	New(enr, st, snd, "1", dd(), cd(), quiet()).Handle(context.Background(), trade)

	if snd.calls != 1 {
		t.Fatalf("send calls = %d, want 1 (stats failure must not block alert)", snd.calls)
	}
	if strings.Contains(snd.text, "👤") {
		t.Errorf("stats line should be omitted on error: %q", snd.text)
	}
}

func TestHandle_EnrichmentNotFound_StillAlerts(t *testing.T) {
	enr := fakeEnricher{err: status.Error(codes.NotFound, "nope")}
	snd := &fakeSender{}
	New(enr, nil, snd, "1", dd(), cd(), quiet()).Handle(context.Background(), trade)

	if snd.calls != 1 {
		t.Fatalf("send calls = %d, want 1 (degrade gracefully)", snd.calls)
	}
	if !strings.Contains(snd.text, "Unknown market") {
		t.Errorf("NotFound should say unknown market: %q", snd.text)
	}
}

func TestHandle_EnrichmentTransient_LookupUnavailable(t *testing.T) {
	enr := fakeEnricher{err: status.Error(codes.Unavailable, "enrichment down")}
	snd := &fakeSender{}
	New(enr, nil, snd, "1", dd(), cd(), quiet()).Handle(context.Background(), trade)

	if snd.calls != 1 {
		t.Fatalf("send calls = %d, want 1 (still alerts)", snd.calls)
	}
	if !strings.Contains(snd.text, "lookup unavailable") {
		t.Errorf("transient error should say lookup unavailable, not unknown market: %q", snd.text)
	}
}

func TestHandle_SendFailure_NoPanic(t *testing.T) {
	enr := fakeEnricher{resp: &lobsterrollv1.EnrichTokenResponse{MarketQuestion: "Q", Outcome: "Yes"}}
	snd := &fakeSender{err: errors.New("telegram down")}
	// Must not panic or block; error is logged internally.
	New(enr, nil, snd, "1", dd(), cd(), quiet()).Handle(context.Background(), trade)
	if snd.calls != 1 {
		t.Fatalf("send attempted = %d, want 1", snd.calls)
	}
}

func TestHandleConsensus_Found(t *testing.T) {
	enr := fakeEnricher{resp: &lobsterrollv1.EnrichTokenResponse{MarketQuestion: "Ghana vs. Panama: O/U 2.5", Outcome: "Over", MarketSlug: "fifwc-gha-pan-total-2pt5"}}
	snd := &fakeSender{}
	sig := bus.ConsensusSignal{
		TokenID: "2596", Side: "buy", Wallets: []string{"a", "b", "c", "d"},
		Count: 4, CombinedUSD: 12000, WindowSecs: 6 * 3600,
	}
	New(enr, nil, snd, "777", dd(), cd(), quiet()).HandleConsensus(context.Background(), sig)

	if snd.calls != 1 {
		t.Fatalf("send calls = %d, want 1", snd.calls)
	}
	if snd.chatID != "777" {
		t.Errorf("chatID = %q, want 777", snd.chatID)
	}
	if !strings.Contains(snd.text, "🔥 CONSENSUS — 4 tracked wallets BUY") {
		t.Errorf("text missing consensus header: %q", snd.text)
	}
	if !strings.Contains(snd.text, "Ghana vs. Panama: O/U 2.5 → Over") {
		t.Errorf("text missing market: %q", snd.text)
	}
	if !strings.Contains(snd.text, "4 wallets · combined $12.0k · 6h window") {
		t.Errorf("text missing tally: %q", snd.text)
	}
}

func TestHandleConsensus_EnrichmentNotFound_StillAlerts(t *testing.T) {
	enr := fakeEnricher{err: status.Error(codes.NotFound, "nope")}
	snd := &fakeSender{}
	sig := bus.ConsensusSignal{TokenID: "2596", Side: "sell", Count: 2, CombinedUSD: 500, WindowSecs: 1800}
	New(enr, nil, snd, "1", dd(), cd(), quiet()).HandleConsensus(context.Background(), sig)

	if snd.calls != 1 {
		t.Fatalf("send calls = %d, want 1 (degrade gracefully)", snd.calls)
	}
	if !strings.Contains(snd.text, "Unknown market") {
		t.Errorf("NotFound should say unknown market: %q", snd.text)
	}
}
