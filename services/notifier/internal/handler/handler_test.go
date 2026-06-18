package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/pkg/bus"
)

type fakeEnricher struct {
	resp *lobsterrollv1.EnrichTokenResponse
	err  error
}

func (f fakeEnricher) EnrichToken(context.Context, *lobsterrollv1.EnrichTokenRequest, ...grpc.CallOption) (*lobsterrollv1.EnrichTokenResponse, error) {
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

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

var trade = bus.TradeDetected{
	Wallet:  "0x037c0f46600702e77ccb738721a78d6418d3a458",
	TokenID: "2596", Side: "buy", Price: "0.95", Size: "5.76",
	TxHash: "0x7ccd161ea4de1234567890abcdef1234567890abcdef1234567890abcdef1234",
}

func TestHandle_Enriched(t *testing.T) {
	enr := fakeEnricher{resp: &lobsterrollv1.EnrichTokenResponse{MarketQuestion: "Ghana vs. Panama: O/U 2.5", Outcome: "Over"}}
	snd := &fakeSender{}
	New(enr, snd, "999", quiet()).Handle(context.Background(), trade)

	if snd.calls != 1 {
		t.Fatalf("send calls = %d, want 1", snd.calls)
	}
	if snd.chatID != "999" {
		t.Errorf("chatID = %q, want 999", snd.chatID)
	}
	if !strings.Contains(snd.text, "Ghana vs. Panama: O/U 2.5 — Over") {
		t.Errorf("text missing market: %q", snd.text)
	}
	if !strings.Contains(snd.text, "🟢 BUY") {
		t.Errorf("text missing buy marker: %q", snd.text)
	}
}

func TestHandle_EnrichmentNotFound_StillAlerts(t *testing.T) {
	enr := fakeEnricher{err: status.Error(codes.NotFound, "nope")}
	snd := &fakeSender{}
	New(enr, snd, "1", quiet()).Handle(context.Background(), trade)

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
	New(enr, snd, "1", quiet()).Handle(context.Background(), trade)

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
	New(enr, snd, "1", quiet()).Handle(context.Background(), trade)
	if snd.calls != 1 {
		t.Fatalf("send attempted = %d, want 1", snd.calls)
	}
}
