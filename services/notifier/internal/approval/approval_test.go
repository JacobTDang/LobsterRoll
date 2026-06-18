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

const opChat = 55 // matches newMgr's chatID "55"

func cbData(tg *fakeTG, idx int) string {
	tg.mu.Lock()
	defer tg.mu.Unlock()
	return tg.lastKB[0][idx].CallbackData
}

// cbFor builds a callback for the last-sent proposal's button (idx 0=approve,
// 1=reject) from the authorized operator chat.
func cbFor(tg *fakeTG, idx int, from string) telegram.CallbackQuery {
	return telegram.CallbackQuery{
		ID: "cb", Data: cbData(tg, idx),
		From:    telegram.User{Username: from},
		Message: &telegram.Message{Chat: telegram.Chat{ID: opChat}},
	}
}

var proposal2 = bus.OrderProposal{ID: "prop-Y", TokenID: "tok2", Side: "sell", LimitPrice: "0.40", SizeUSD: 10}

func TestOnProposal_SendsButtons(t *testing.T) {
	m, tg, _ := newMgr()
	m.OnProposal(context.Background(), proposal)
	if tg.keyboards != 1 {
		t.Fatalf("keyboards = %d, want 1", tg.keyboards)
	}
	a, r := cbData(tg, 0), cbData(tg, 1)
	ka, _, ok1 := ParseCallback(a)
	kr, _, ok2 := ParseCallback(r)
	_ = ka
	_ = kr
	if !ok1 || !ok2 || a[:2] != "a:" || r[:2] != "r:" || a[2:] != r[2:] {
		t.Fatalf("buttons = %q / %q, want a:<key> / r:<key> with same key", a, r)
	}

	// A different proposal must get a different (id-derived) key.
	m.OnProposal(context.Background(), proposal2)
	if cbData(tg, 0)[2:] == a[2:] {
		t.Fatalf("different proposals share a callback key: %q", a)
	}
}

func TestOnProposal_KeyStableAcrossInstances(t *testing.T) {
	// Two independent managers (simulating a restart) derive the same key for the
	// same proposal id, so a stale button can never alias a new proposal.
	m1, tg1, _ := newMgr()
	m2, tg2, _ := newMgr()
	m1.OnProposal(context.Background(), proposal)
	m2.OnProposal(context.Background(), proposal)
	if cbData(tg1, 0) != cbData(tg2, 0) {
		t.Fatalf("key not stable across instances: %q vs %q", cbData(tg1, 0), cbData(tg2, 0))
	}
}

func TestOnProposal_NoCrossProposalCollisionAcrossRestart(t *testing.T) {
	// The bug: a counter resets on restart, so a DIFFERENT proposal posted after
	// a restart reuses an old key and a stale button decides the wrong proposal.
	// With id-derived keys, different proposals never share a key.
	m1, tg1, _ := newMgr()
	m2, tg2, _ := newMgr() // fresh instance == restart
	m1.OnProposal(context.Background(), proposal)
	m2.OnProposal(context.Background(), proposal2)
	if cbData(tg1, 0) == cbData(tg2, 0) {
		t.Fatalf("different proposals reuse the same callback key after restart: %q", cbData(tg1, 0))
	}
}

func TestHandleCallback_Approve(t *testing.T) {
	m, tg, pub := newMgr()
	m.OnProposal(context.Background(), proposal)

	m.HandleCallback(context.Background(), cbFor(tg, 0, "me"))

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
	m, tg, pub := newMgr()
	m.OnProposal(context.Background(), proposal)
	m.HandleCallback(context.Background(), cbFor(tg, 1, "me"))
	if pub.decisionCount() != 1 || pub.decisions[0].Approved {
		t.Fatalf("decision = %+v, want rejected", pub.decisions)
	}
}

func TestHandleCallback_Idempotent(t *testing.T) {
	m, tg, pub := newMgr()
	m.OnProposal(context.Background(), proposal)
	cb := cbFor(tg, 0, "me")
	m.HandleCallback(context.Background(), cb)
	m.HandleCallback(context.Background(), cb) // tapped twice
	if pub.decisionCount() != 1 {
		t.Fatalf("decisions = %d, want 1 (idempotent)", pub.decisionCount())
	}
	if last := tg.answers[len(tg.answers)-1]; last != "Already decided" {
		t.Errorf("second tap answer = %q, want Already decided", last)
	}
}

func TestHandleCallback_Expired(t *testing.T) {
	m, _, pub := newMgr()
	// No OnProposal: key unknown, but from the authorized chat.
	cb := telegram.CallbackQuery{ID: "c", Data: "a:deadbeefdeadbeef", From: telegram.User{Username: "me"}, Message: &telegram.Message{Chat: telegram.Chat{ID: opChat}}}
	m.HandleCallback(context.Background(), cb)
	if pub.decisionCount() != 0 {
		t.Fatalf("decisions = %d, want 0 for expired", pub.decisionCount())
	}
}

func TestHandleCallback_UnauthorizedChat(t *testing.T) {
	m, tg, pub := newMgr()
	m.OnProposal(context.Background(), proposal)
	// Same valid key, but from a different chat.
	cb := telegram.CallbackQuery{ID: "c", Data: cbData(tg, 0), From: telegram.User{Username: "intruder"}, Message: &telegram.Message{Chat: telegram.Chat{ID: 999}}}
	m.HandleCallback(context.Background(), cb)
	if pub.decisionCount() != 0 {
		t.Fatalf("decisions = %d, want 0 from unauthorized chat", pub.decisionCount())
	}
}

func TestHandleCommand_HaltResume(t *testing.T) {
	m, _, pub := newMgr()
	ctx := context.Background()

	m.HandleCommand(ctx, "/halt", opChat, "me")
	if !m.Halted() {
		t.Fatal("expected halted after /halt")
	}
	m.HandleCommand(ctx, "/resume", opChat, "me")
	if m.Halted() {
		t.Fatal("expected not halted after /resume")
	}
	if len(pub.controls) != 2 || !pub.controls[0].Halted || pub.controls[1].Halted {
		t.Fatalf("controls = %+v, want [halt resume]", pub.controls)
	}
}

func TestHandleCommand_Unauthorized(t *testing.T) {
	m, _, pub := newMgr()
	m.HandleCommand(context.Background(), "/halt", 999, "intruder")
	if m.Halted() {
		t.Fatal("/halt from unauthorized chat must not halt")
	}
	if len(pub.controls) != 0 {
		t.Fatalf("controls = %d, want 0 from unauthorized chat", len(pub.controls))
	}
}
