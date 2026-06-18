// Package approval implements the two-way Telegram approval gate: it posts
// proposals with ✅/❌ buttons, turns button taps into orders.approved /
// orders.rejected, and handles /halt and /resume (control.halt). Decisions are
// idempotent (a proposal can be decided at most once).
package approval

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/format"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/telegram"
)

// Decider publishes decisions and control messages.
type Decider interface {
	PublishDecision(bus.OrderDecision) error
	PublishControl(bus.ControlMsg) error
}

// Telegram is the subset of the bot client the manager needs.
type Telegram interface {
	SendKeyboard(ctx context.Context, chatID, text string, keyboard [][]telegram.InlineButton) (int, error)
	AnswerCallback(ctx context.Context, callbackID, text string) error
	EditMessageText(ctx context.Context, chatID string, messageID int, text string) error
	Send(ctx context.Context, chatID, text string) error
}

type pending struct {
	proposalID string
	messageID  int
	text       string
}

// Manager coordinates proposal approval over Telegram.
type Manager struct {
	tg     Telegram
	pub    Decider
	chatID string
	log    *slog.Logger

	mu      sync.Mutex
	nextKey int
	pend    map[string]pending // shortKey -> pending
	decided map[string]bool    // shortKey -> decided
	halted  bool
}

// New constructs a Manager.
func New(tg Telegram, pub Decider, chatID string, log *slog.Logger) *Manager {
	return &Manager{
		tg: tg, pub: pub, chatID: chatID, log: log,
		pend: map[string]pending{}, decided: map[string]bool{},
	}
}

// OnProposal posts a proposal with approve/reject buttons.
func (m *Manager) OnProposal(ctx context.Context, p bus.OrderProposal) {
	text := format.FormatProposal(p)

	m.mu.Lock()
	m.nextKey++
	key := strconv.Itoa(m.nextKey)
	m.mu.Unlock()

	kb := [][]telegram.InlineButton{{
		{Text: "✅ Approve", CallbackData: "a:" + key},
		{Text: "❌ Reject", CallbackData: "r:" + key},
	}}
	msgID, err := m.tg.SendKeyboard(ctx, m.chatID, text, kb)
	if err != nil {
		m.log.Error("send proposal failed", "id", p.ID, "err", err)
		return
	}
	m.mu.Lock()
	m.pend[key] = pending{proposalID: p.ID, messageID: msgID, text: text}
	m.mu.Unlock()
	m.log.Info("proposal awaiting approval", "id", p.ID, "key", key)
}

// HandleCallback turns a button tap into a decision (idempotently).
func (m *Manager) HandleCallback(ctx context.Context, cb telegram.CallbackQuery) {
	action, key, ok := ParseCallback(cb.Data)
	if !ok {
		_ = m.tg.AnswerCallback(ctx, cb.ID, "Unrecognized action")
		return
	}

	m.mu.Lock()
	p, found := m.pend[key]
	if !found {
		m.mu.Unlock()
		_ = m.tg.AnswerCallback(ctx, cb.ID, "Proposal expired")
		return
	}
	if m.decided[key] {
		m.mu.Unlock()
		_ = m.tg.AnswerCallback(ctx, cb.ID, "Already decided")
		return
	}
	m.decided[key] = true
	m.mu.Unlock()

	approved := action == "a"
	by := "telegram:" + cb.From.Username
	if err := m.pub.PublishDecision(bus.OrderDecision{ProposalID: p.proposalID, Approved: approved, By: by}); err != nil {
		m.log.Error("publish decision failed", "id", p.proposalID, "err", err)
		// Roll back so the operator can retry the tap.
		m.mu.Lock()
		m.decided[key] = false
		m.mu.Unlock()
		_ = m.tg.AnswerCallback(ctx, cb.ID, "Failed — try again")
		return
	}

	verb, mark := "Rejected", "❌"
	if approved {
		verb, mark = "Approved", "✅"
	}
	_ = m.tg.AnswerCallback(ctx, cb.ID, verb)
	_ = m.tg.EditMessageText(ctx, m.chatID, p.messageID, fmt.Sprintf("%s %s by @%s\n%s", mark, verb, cb.From.Username, p.text))
	m.log.Info("decision", "id", p.proposalID, "approved", approved, "by", by)
}

// HandleCommand handles /halt and /resume.
func (m *Manager) HandleCommand(ctx context.Context, text, fromUsername string) {
	by := "telegram:" + fromUsername
	switch strings.TrimSpace(text) {
	case "/halt":
		m.mu.Lock()
		m.halted = true
		m.mu.Unlock()
		if err := m.pub.PublishControl(bus.ControlMsg{Halted: true, By: by}); err != nil {
			m.log.Error("publish halt failed", "err", err)
		}
		_ = m.tg.Send(ctx, m.chatID, "🛑 HALTED — downstream execution paused.")
	case "/resume":
		m.mu.Lock()
		m.halted = false
		m.mu.Unlock()
		if err := m.pub.PublishControl(bus.ControlMsg{Halted: false, By: by}); err != nil {
			m.log.Error("publish resume failed", "err", err)
		}
		_ = m.tg.Send(ctx, m.chatID, "▶️ RESUMED — execution re-enabled.")
	}
}

// Halted reports the current halt state.
func (m *Manager) Halted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.halted
}

// ParseCallback parses "a:<key>" / "r:<key>" into action ("a"|"r") and key.
func ParseCallback(data string) (action, key string, ok bool) {
	parts := strings.SplitN(data, ":", 2)
	if len(parts) != 2 || (parts[0] != "a" && parts[0] != "r") || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
