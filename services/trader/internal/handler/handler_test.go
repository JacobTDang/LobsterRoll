package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/pkg/config"
	"github.com/JacobTDang/LobsterRoll/services/trader/internal/caps"
	"github.com/JacobTDang/LobsterRoll/services/trader/internal/clob"
	"github.com/JacobTDang/LobsterRoll/services/trader/internal/halt"
)

type fakeCaps struct {
	allow    bool
	reason   string
	reserved int
	released int
}

func (f *fakeCaps) Reserve(float64, bool) caps.Decision {
	f.reserved++
	return caps.Decision{Allowed: f.allow, Reason: f.reason}
}
func (f *fakeCaps) Release(float64, bool) { f.released++ }

type fakeSigner struct{ err error }

func (f fakeSigner) Sign(bus.OrderProposal) (clob.SignedOrder, error) {
	return clob.SignedOrder{Signature: "0xsig"}, f.err
}

type fakePlacer struct {
	res   clob.PlaceResult
	err   error
	calls int
}

func (f *fakePlacer) PlaceOrder(context.Context, clob.SignedOrder) (clob.PlaceResult, error) {
	f.calls++
	return f.res, f.err
}

type fakeStore struct {
	mu      sync.Mutex
	claimed map[string]bool
	err     error
}

func newStore() *fakeStore { return &fakeStore{claimed: map[string]bool{}} }
func (s *fakeStore) Claim(_ context.Context, id string) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimed[id] {
		return false, nil
	}
	s.claimed[id] = true
	return true, nil
}
func (s *fakeStore) MarkResult(context.Context, string, string, string) error { return nil }

type fakePub struct {
	mu      sync.Mutex
	results []bus.OrderResult
}

func (p *fakePub) PublishResult(r bus.OrderResult) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.results = append(p.results, r)
	return nil
}
func (p *fakePub) last() bus.OrderResult { p.mu.Lock(); defer p.mu.Unlock(); return p.results[len(p.results)-1] }

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

var proposal = bus.OrderProposal{ID: "prop-1", TokenID: "123", Side: "buy", LimitPrice: "0.50", SizeUSD: 25}

func newHandler(allow bool) (*Handler, *fakePlacer, *fakePub, *fakeCaps, *halt.State) {
	c := &fakeCaps{allow: allow, reason: "per-trade cap"}
	pl := &fakePlacer{res: clob.PlaceResult{Success: true, OrderID: "ord-1", Status: "matched"}}
	pub := &fakePub{}
	h := halt.New()
	pol := config.ExecutionPolicy{Mode: config.ModeApproval}
	return New(c, fakeSigner{}, pl, newStore(), pub, h, pol, quiet()), pl, pub, c, h
}

func TestPlace_Approved_Fills(t *testing.T) {
	h, pl, pub, _, _ := newHandler(true)
	h.OnApproved(context.Background(), bus.OrderDecision{Proposal: proposal, Approved: true, By: "telegram:me"})
	if pl.calls != 1 {
		t.Fatalf("placer calls = %d, want 1", pl.calls)
	}
	r := pub.last()
	if !r.Filled || r.OrderID != "ord-1" || r.ProposalID != "prop-1" {
		t.Fatalf("result = %+v, want filled ord-1", r)
	}
}

func TestPlace_HaltRefuses(t *testing.T) {
	h, pl, pub, _, hs := newHandler(true)
	hs.Set(true)
	h.OnApproved(context.Background(), bus.OrderDecision{Proposal: proposal, Approved: true})
	if pl.calls != 0 {
		t.Fatalf("placer called while halted: %d", pl.calls)
	}
	if r := pub.last(); r.Filled || r.Err != "halted" {
		t.Fatalf("result = %+v, want failed/halted", r)
	}
}

func TestPlace_CapRejected(t *testing.T) {
	h, pl, pub, c, _ := newHandler(false) // caps deny
	h.OnApproved(context.Background(), bus.OrderDecision{Proposal: proposal, Approved: true})
	if pl.calls != 0 {
		t.Fatalf("placer called despite cap rejection: %d", pl.calls)
	}
	if c.released != 0 {
		t.Fatalf("released %d, want 0 (nothing reserved)", c.released)
	}
	if r := pub.last(); r.Filled {
		t.Fatalf("result = %+v, want failed", r)
	}
}

func TestPlace_Idempotent(t *testing.T) {
	h, pl, _, _, _ := newHandler(true)
	d := bus.OrderDecision{Proposal: proposal, Approved: true}
	h.OnApproved(context.Background(), d)
	h.OnApproved(context.Background(), d) // redelivery
	if pl.calls != 1 {
		t.Fatalf("placer calls = %d, want 1 (idempotent)", pl.calls)
	}
}

func TestPlace_SignErrorReleasesCaps(t *testing.T) {
	c := &fakeCaps{allow: true}
	pl := &fakePlacer{}
	pub := &fakePub{}
	h := New(c, fakeSigner{err: errors.New("bad key")}, pl, newStore(), pub, halt.New(), config.ExecutionPolicy{Mode: config.ModeApproval}, quiet())
	h.OnApproved(context.Background(), bus.OrderDecision{Proposal: proposal, Approved: true})
	if pl.calls != 0 {
		t.Fatalf("placer called after sign error: %d", pl.calls)
	}
	if c.released != 1 {
		t.Fatalf("caps released = %d, want 1 (rollback on sign failure)", c.released)
	}
	if pub.last().Filled {
		t.Fatal("want failed result")
	}
}

func TestPlace_PlaceErrorReleasesCaps(t *testing.T) {
	c := &fakeCaps{allow: true}
	pl := &fakePlacer{err: errors.New("clob 500")}
	pub := &fakePub{}
	h := New(c, fakeSigner{}, pl, newStore(), pub, halt.New(), config.ExecutionPolicy{Mode: config.ModeApproval}, quiet())
	h.OnApproved(context.Background(), bus.OrderDecision{Proposal: proposal, Approved: true})
	if c.released != 1 {
		t.Fatalf("caps released = %d, want 1 (rollback on place failure)", c.released)
	}
	if pub.last().Filled {
		t.Fatal("want failed result")
	}
}

func TestOnProposed_AutoModePlaces(t *testing.T) {
	c := &fakeCaps{allow: true}
	pl := &fakePlacer{res: clob.PlaceResult{Success: true, OrderID: "o"}}
	h := New(c, fakeSigner{}, pl, newStore(), &fakePub{}, halt.New(), config.ExecutionPolicy{Mode: config.ModeAuto}, quiet())
	h.OnProposed(context.Background(), proposal)
	if pl.calls != 1 {
		t.Fatalf("auto mode: placer calls = %d, want 1", pl.calls)
	}
}

func TestOnProposed_ApprovalModeSkips(t *testing.T) {
	h, pl, _, _, _ := newHandler(true) // ModeApproval
	h.OnProposed(context.Background(), proposal)
	if pl.calls != 0 {
		t.Fatalf("approval mode: placer calls = %d, want 0 (awaits approval)", pl.calls)
	}
}
