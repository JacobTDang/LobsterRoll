package signer

import (
	"testing"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
)

const (
	testKey  = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"
	exchange = "0xe111180000d2663c0091e4f400237545b87b996b"
)

func TestSign_RendersPayload(t *testing.T) {
	s, err := New(testKey, "", exchange, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.now = func() time.Time { return time.Unix(1700000000, 0) }

	so, err := s.Sign(bus.OrderProposal{TokenID: "123", Side: "buy", LimitPrice: "0.50", SizeUSD: 25})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if so.Side != "BUY" {
		t.Errorf("side = %q, want BUY", so.Side)
	}
	if so.MakerAmount != "25000000" || so.TakerAmount != "50000000" {
		t.Errorf("amounts = %s/%s, want 25000000/50000000", so.MakerAmount, so.TakerAmount)
	}
	if so.TokenID != "123" || so.Maker == "" || so.Signer == "" {
		t.Errorf("payload = %+v", so)
	}
	if len(so.Signature) != 2+130 { // 0x + 65 bytes hex
		t.Errorf("signature len = %d, want 132", len(so.Signature))
	}
	if so.Metadata != "0x0000000000000000000000000000000000000000000000000000000000000000" {
		t.Errorf("metadata = %q", so.Metadata)
	}
}

func TestSign_BadProposal(t *testing.T) {
	s, _ := New(testKey, "", exchange, 0)
	if _, err := s.Sign(bus.OrderProposal{TokenID: "1", Side: "buy", LimitPrice: "0", SizeUSD: 10}); err == nil {
		t.Error("expected error for zero price")
	}
}

func TestNew_BadKey(t *testing.T) {
	if _, err := New("nothex", "", exchange, 0); err == nil {
		t.Error("expected error for bad key")
	}
}
