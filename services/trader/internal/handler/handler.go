// Package handler executes approved (and, in auto mode, proposed) orders behind
// the trader's independent safety net: halt → idempotency claim → hard caps →
// sign → place → publish result.
package handler

import (
	"context"
	"log/slog"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/pkg/config"
	"github.com/JacobTDang/LobsterRoll/services/trader/internal/caps"
	"github.com/JacobTDang/LobsterRoll/services/trader/internal/clob"
	"github.com/JacobTDang/LobsterRoll/services/trader/internal/halt"
)

// Caps is the hard-cap safety net.
type Caps interface {
	Reserve(sizeUSD float64, buy bool) caps.Decision
	Release(sizeUSD float64, buy bool)
}

// Signer builds and signs the CLOB order from a proposal.
type Signer interface {
	Sign(p bus.OrderProposal) (clob.SignedOrder, error)
}

// Placer places a signed order on the CLOB.
type Placer interface {
	PlaceOrder(ctx context.Context, o clob.SignedOrder) (clob.PlaceResult, error)
}

// Store provides at-most-once placement claims.
type Store interface {
	Claim(ctx context.Context, proposalID string) (bool, error)
	Unclaim(ctx context.Context, proposalID string) error
	MarkResult(ctx context.Context, proposalID, orderID, status string) error
}

// Publisher publishes execution results.
type Publisher interface {
	PublishResult(r bus.OrderResult) error
}

// Handler wires the execution pipeline.
type Handler struct {
	caps   Caps
	signer Signer
	placer Placer
	store  Store
	pub    Publisher
	halt   *halt.State
	policy config.ExecutionPolicy
	log    *slog.Logger
}

// New constructs a Handler.
func New(c Caps, s Signer, p Placer, st Store, pub Publisher, h *halt.State, policy config.ExecutionPolicy, log *slog.Logger) *Handler {
	return &Handler{caps: c, signer: s, placer: p, store: st, pub: pub, halt: h, policy: policy, log: log}
}

// OnApproved executes an approved decision.
func (h *Handler) OnApproved(ctx context.Context, d bus.OrderDecision) {
	if !d.Approved {
		return
	}
	h.place(ctx, d.Proposal, "approved "+d.By)
}

// OnProposed auto-executes a proposal that does not require approval under the
// current execution policy (auto / auto_below). Proposals needing approval are
// ignored here and flow through the approval gate instead.
func (h *Handler) OnProposed(ctx context.Context, p bus.OrderProposal) {
	if h.policy.RequiresApproval(p.SizeUSD) {
		return
	}
	h.place(ctx, p, "auto")
}

func (h *Handler) place(ctx context.Context, p bus.OrderProposal, source string) {
	// 1. Kill switch — refuse before touching anything.
	if h.halt.Halted() {
		h.fail(p, "halted")
		return
	}
	// 2. At-most-once: claim before placing so a proposal is never placed twice.
	claimed, err := h.store.Claim(ctx, p.ID)
	if err != nil {
		h.fail(p, "claim error: "+err.Error())
		return
	}
	if !claimed {
		return // already handled
	}
	// 3. Independent hard caps (the last safety net).
	buy := p.Side == "buy"
	if dec := h.caps.Reserve(p.SizeUSD, buy); !dec.Allowed {
		h.fail(p, "cap: "+dec.Reason)
		return
	}
	// 4. Sign. A sign failure is definitely-not-sent → unclaim so it can retry.
	so, err := h.signer.Sign(p)
	if err != nil {
		h.caps.Release(p.SizeUSD, buy)
		h.unclaim(ctx, p.ID)
		h.fail(p, "sign: "+err.Error())
		return
	}
	// 5. Re-check the kill switch immediately before placing (it may have flipped
	// during signing). Nothing has been sent yet → unclaim so it can retry.
	if h.halt.Halted() {
		h.caps.Release(p.SizeUSD, buy)
		h.unclaim(ctx, p.ID)
		h.fail(p, "halted")
		return
	}
	// 6. Place. A place error is ambiguous (the order may have reached the book),
	// so mark it failed and keep the claim — no auto-retry; reconcile out of band.
	res, err := h.placer.PlaceOrder(ctx, so)
	if err != nil {
		h.caps.Release(p.SizeUSD, buy)
		if merr := h.store.MarkResult(ctx, p.ID, "", "failed"); merr != nil {
			h.log.Error("mark failed-result failed", "id", p.ID, "err", merr)
		}
		h.fail(p, "place: "+err.Error())
		return
	}
	if merr := h.store.MarkResult(ctx, p.ID, res.OrderID, res.Status); merr != nil {
		// Critical: a real order placed but we couldn't persist it.
		h.log.Error("CRITICAL: placed order but failed to persist result", "id", p.ID, "order", res.OrderID, "err", merr)
	}
	if err := h.pub.PublishResult(bus.OrderResult{ProposalID: p.ID, OrderID: res.OrderID, Filled: true}); err != nil {
		h.log.Error("CRITICAL: placed order but failed to publish filled", "id", p.ID, "order", res.OrderID, "err", err)
	}
	h.log.Info("order placed", "id", p.ID, "order", res.OrderID, "status", res.Status, "source", source, "sizeUSD", p.SizeUSD)
}

func (h *Handler) unclaim(ctx context.Context, id string) {
	if err := h.store.Unclaim(ctx, id); err != nil {
		h.log.Error("unclaim failed", "id", id, "err", err)
	}
}

func (h *Handler) fail(p bus.OrderProposal, reason string) {
	if err := h.pub.PublishResult(bus.OrderResult{ProposalID: p.ID, Filled: false, Err: reason}); err != nil {
		h.log.Error("publish failed-result failed", "id", p.ID, "err", err)
	}
	h.log.Warn("order not placed", "id", p.ID, "reason", reason)
}
