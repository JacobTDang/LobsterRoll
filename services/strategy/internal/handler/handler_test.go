package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/services/strategy/internal/decide"
	"github.com/JacobTDang/LobsterRoll/services/strategy/internal/marketdata"
)

type fakeSrc struct {
	data marketdata.Data
	ok   bool
	err  error
	mu   sync.Mutex
	hits int
}

func (s *fakeSrc) Fetch(context.Context, string) (marketdata.Data, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hits++
	return s.data, s.ok, s.err
}

type fakeProposer struct {
	mu   sync.Mutex
	got  []bus.OrderProposal
}

func (p *fakeProposer) PublishProposal(o bus.OrderProposal) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.got = append(p.got, o)
	return nil
}
func (p *fakeProposer) count() int { p.mu.Lock(); defer p.mu.Unlock(); return len(p.got) }

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

var policy = decide.Policy{
	Sizing: decide.SizingFixed, FixedUSD: 25, MinSizeUSD: 5, MaxSizeUSD: 100,
	MaxSlippage: 0.03, MinLiquidityUSD: 1000,
}

var trade = bus.TradeDetected{
	Wallet: "0xwhale", TokenID: "tok", Side: "buy", Price: "0.95", Size: "5.76",
	TxHash: "0xabc", LogIndex: 7,
}

func goodData() marketdata.Data {
	return marketdata.Data{CurrentPrice: 0.96, LiquidityUSD: 5000, ConditionID: "0xc", Active: true}
}

func TestHandle_Proposes(t *testing.T) {
	pub := &fakeProposer{}
	h := New(&fakeSrc{data: goodData(), ok: true}, pub, policy, nil, quiet())
	h.Handle(context.Background(), trade)

	if pub.count() != 1 {
		t.Fatalf("proposals = %d, want 1", pub.count())
	}
	p := pub.got[0]
	if p.ID != "prop-0xabc-7-0xwhale" || p.Side != "buy" || p.SizeUSD != 25 || p.LimitPrice != "0.98" {
		t.Fatalf("proposal = %+v", p)
	}
}

func TestHandle_Idempotent(t *testing.T) {
	pub := &fakeProposer{}
	h := New(&fakeSrc{data: goodData(), ok: true}, pub, policy, nil, quiet())
	h.Handle(context.Background(), trade)
	h.Handle(context.Background(), trade) // redelivery
	if pub.count() != 1 {
		t.Fatalf("proposals = %d, want 1 (idempotent)", pub.count())
	}
}

func TestHandle_SkipNotFound(t *testing.T) {
	pub := &fakeProposer{}
	New(&fakeSrc{ok: false}, pub, policy, nil, quiet()).Handle(context.Background(), trade)
	if pub.count() != 0 {
		t.Fatalf("proposals = %d, want 0 (market not found)", pub.count())
	}
}

func TestHandle_SkipSlippage(t *testing.T) {
	d := goodData()
	d.CurrentPrice = 1.00 // far above whale 0.95 + 0.03
	pub := &fakeProposer{}
	New(&fakeSrc{data: d, ok: true}, pub, policy, nil, quiet()).Handle(context.Background(), trade)
	if pub.count() != 0 {
		t.Fatalf("proposals = %d, want 0 (slippage)", pub.count())
	}
}

func TestHandle_Allowlist(t *testing.T) {
	pub := &fakeProposer{}
	// Allowlist that does NOT include the market's condition id.
	h := New(&fakeSrc{data: goodData(), ok: true}, pub, policy, map[string]bool{"0xother": true}, quiet())
	h.Handle(context.Background(), trade)
	if pub.count() != 0 {
		t.Fatalf("proposals = %d, want 0 (not in allowlist)", pub.count())
	}
}

func TestHandle_TransientErrorRetryable(t *testing.T) {
	src := &fakeSrc{err: errors.New("gamma down")}
	pub := &fakeProposer{}
	h := New(src, pub, policy, nil, quiet())
	h.Handle(context.Background(), trade) // fails transiently, not marked seen
	if pub.count() != 0 {
		t.Fatalf("proposals = %d, want 0 on error", pub.count())
	}
	// Recover the source: a redelivery must now succeed (proves not marked seen).
	src.err = nil
	src.data = goodData()
	src.ok = true
	h.Handle(context.Background(), trade)
	if pub.count() != 1 {
		t.Fatalf("proposals = %d, want 1 after recovery (transient was retryable)", pub.count())
	}
}
