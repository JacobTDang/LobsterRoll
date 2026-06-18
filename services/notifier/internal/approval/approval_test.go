package approval

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/telegram"
)

type fakeTG struct {
	mu        sync.Mutex
	keyboards int
	lastKB    [][]telegram.InlineButton
	answers   []string
	edits     []string
	sends     []string
	nextMsgID int
}

func (f *fakeTG) SendKeyboard(_ context.Context, _, _ string, kb [][]telegram.InlineButton) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keyboards++
	f.lastKB = kb
	f.nextMsgID++
	return f.nextMsgID, nil
}
func (f *fakeTG) AnswerCallback(_ context.Context, _, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answers = append(f.answers, text)
	return nil
}
func (f *fakeTG) EditMessageText(_ context.Context, _ string, _ int, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.edits = append(f.edits, text)
	return nil
}
func (f *fakeTG) Send(_ context.Context, _, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sends = append(f.sends, text)
	return nil
}

type fakePub struct {
	mu        sync.Mutex
	decisions []bus.OrderDecision
	controls  []bus.ControlMsg
}

func (p *fakePub) PublishDecision(d bus.OrderDecision) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.decisions = append(p.decisions, d)
	return nil
}
func (p *fakePub) PublishControl(c bus.ControlMsg) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.controls = append(p.controls, c)
	return nil
}
func (p *fakePub) decisionCount() int { p.mu.Lock(); defer p.mu.Unlock(); return len(p.decisions) }

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

var proposal = bus.OrderProposal{ID: "prop-X", TokenID: "tok", Side: "buy", LimitPrice: "0.98", SizeUSD: 25}

func newMgr() (*Manager, *fakeTG, *fakePub) {
	tg, pub := &fakeTG{}, &fakePub{}
	return New(tg, pub, "55", quiet()), tg, pub
}

func TestParseCallback(t *testing.T) {
	tests := []struct {
		in            string
		action, key   string
		ok            bool
	}{
		{"a:7", "a", "7", true},
		{"r:42", "r", "42", true},
		{"x:7", "", "", false},
		{"a:", "", "", false},
		{"a", "", "", false},
		{"", "", "", false},
	}
	for _, tt := range tests {
		a, k, ok := ParseCallback(tt.in)
		if a != tt.action || k != tt.key || ok != tt.ok {
			t.Errorf("ParseCallback(%q) = (%q,%q,%v), want (%q,%q,%v)", tt.in, a, k, ok, tt.action, tt.key, tt.ok)
		}
	}
}

func TestOnProposal_SendsButtons(t *testing.T) {
	m, tg, _ := newMgr()
	m.OnProposal(context.Background(), proposal)
	if tg.keyboards != 1 {
		t.Fatalf("keyboards = %d, want 1", tg.keyboards)
	}
	row := tg.lastKB[0]
	if row[0].CallbackData != "a:1" || row[1].CallbackData != "r:1" {
		t.Fatalf("buttons = %+v, want a:1 / r:1", row)
	}
}

func TestHandleCallback_Approve(t *testing.T) {
	m, tg, pub := newMgr()
	m.OnProposal(context.Background(), proposal)

	cb := telegram.CallbackQuery{ID: "cb1", Data: "a:1", From: telegram.User{Username: "me"}}
	m.HandleCallback(context.Background(), cb)

	if pub.decisionCount() != 1 {
		t.Fatalf("decisions = %d, want 1", pub.decisionCount())
	}
	d := pub.decisions[0]
	if d.ProposalID != "prop-X" || !d.Approved || d.By != "telegram:me" {
		t.Fatalf("decision = %+v", d)
	}
	if len(tg.answers) != 1 || tg.answers[0] != "Approved" {
		t.Errorf("answers = %v", tg.answers)
	}
	if len(tg.edits) != 1 {
		t.Errorf("edits = %v, want 1 (buttons stripped)", tg.edits)
	}
}

func TestHandleCallback_Reject(t *testing.T) {
	m, _, pub := newMgr()
	m.OnProposal(context.Background(), proposal)
	m.HandleCallback(context.Background(), telegram.CallbackQuery{ID: "c", Data: "r:1", From: telegram.User{Username: "me"}})
	if pub.decisionCount() != 1 || pub.decisions[0].Approved {
		t.Fatalf("decision = %+v, want rejected", pub.decisions)
	}
}

func TestHandleCallback_Idempotent(t *testing.T) {
	m, tg, pub := newMgr()
	m.OnProposal(context.Background(), proposal)
	cb := telegram.CallbackQuery{ID: "c", Data: "a:1", From: telegram.User{Username: "me"}}
	m.HandleCallback(context.Background(), cb)
	m.HandleCallback(context.Background(), cb) // tapped twice
	if pub.decisionCount() != 1 {
		t.Fatalf("decisions = %d, want 1 (idempotent)", pub.decisionCount())
	}
	last := tg.answers[len(tg.answers)-1]
	if last != "Already decided" {
		t.Errorf("second tap answer = %q, want Already decided", last)
	}
}

func TestHandleCallback_Expired(t *testing.T) {
	m, _, pub := newMgr()
	// No OnProposal: key unknown.
	m.HandleCallback(context.Background(), telegram.CallbackQuery{ID: "c", Data: "a:99", From: telegram.User{Username: "me"}})
	if pub.decisionCount() != 0 {
		t.Fatalf("decisions = %d, want 0 for expired", pub.decisionCount())
	}
}

func TestHandleCommand_HaltResume(t *testing.T) {
	m, _, pub := newMgr()
	ctx := context.Background()

	m.HandleCommand(ctx, "/halt", "me")
	if !m.Halted() {
		t.Fatal("expected halted after /halt")
	}
	m.HandleCommand(ctx, "/resume", "me")
	if m.Halted() {
		t.Fatal("expected not halted after /resume")
	}
	if len(pub.controls) != 2 || !pub.controls[0].Halted || pub.controls[1].Halted {
		t.Fatalf("controls = %+v, want [halt resume]", pub.controls)
	}
}
