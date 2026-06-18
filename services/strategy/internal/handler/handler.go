// Package handler consumes detected trades, vets them against live market data
// and the policy, and publishes order proposals (or logs a skip).
package handler

import (
	"context"
	"log/slog"
	"strings"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/services/strategy/internal/decide"
	"github.com/JacobTDang/LobsterRoll/services/strategy/internal/marketdata"
	"github.com/JacobTDang/LobsterRoll/services/strategy/internal/seen"
)

// MarketSource provides live market data for a token.
type MarketSource interface {
	Fetch(ctx context.Context, tokenID string) (marketdata.Data, bool, error)
}

// Proposer publishes vetted order proposals.
type Proposer interface {
	PublishProposal(o bus.OrderProposal) error
}

// Handler vets trades and emits proposals.
type Handler struct {
	src       MarketSource
	pub       Proposer
	seen      *seen.Set
	policy    decide.Policy
	allowlist map[string]bool // condition ids; empty => allow all
	log       *slog.Logger
}

// New constructs a Handler. An empty allowlist means all markets are allowed.
func New(src MarketSource, pub Proposer, policy decide.Policy, allowlist map[string]bool, log *slog.Logger) *Handler {
	return &Handler{src: src, pub: pub, seen: seen.New(), policy: policy, allowlist: allowlist, log: log}
}

// Handle vets one detected trade. Transient market-data errors are retryable
// (not marked seen); every terminal decision (propose or skip) is recorded so a
// trade yields at most one proposal.
func (h *Handler) Handle(ctx context.Context, td bus.TradeDetected) {
	if td.TokenID == "" {
		return
	}

	data, ok, err := h.src.Fetch(ctx, td.TokenID)
	if err != nil {
		// Transient: don't mark seen so a redelivery can retry.
		h.log.Warn("market data fetch failed; will retry on redelivery", "token", td.TokenID, "err", err)
		return
	}

	id := decide.ProposalID(td)
	if !h.seen.Add(id) {
		return // already handled this source trade
	}

	if !ok {
		h.log.Info("skip: market not found", "token", td.TokenID)
		return
	}

	// Compare case-insensitively: allowlist keys are lowercased at load time.
	allowed := len(h.allowlist) == 0 || h.allowlist[strings.ToLower(data.ConditionID)]
	market := decide.Market{
		CurrentPrice: data.CurrentPrice,
		LiquidityUSD: data.LiquidityUSD,
		ConditionID:  data.ConditionID,
		Active:       data.Active,
		Allowed:      allowed,
	}

	out := decide.Decide(td, market, h.policy)
	if !out.Propose {
		h.log.Info("skip", "reason", out.Reason, "wallet", td.Wallet, "token", td.TokenID)
		return
	}
	if err := h.pub.PublishProposal(out.Proposal); err != nil {
		h.log.Error("publish proposal failed", "id", out.Proposal.ID, "err", err)
		return
	}
	h.log.Info("proposed", "id", out.Proposal.ID, "side", out.Proposal.Side,
		"sizeUSD", out.Proposal.SizeUSD, "limit", out.Proposal.LimitPrice)
}
