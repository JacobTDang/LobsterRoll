// Package decide is the pure decision core of strategy-svc: it turns a detected
// trade plus market context into a vetted order proposal, or a skip with reason.
// No I/O — every function here is deterministic and table-tested.
package decide

import (
	"fmt"
	"math"
	"strconv"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
)

// SizingMode selects how a proposal's USD size is computed.
type SizingMode int

const (
	SizingFixed        SizingMode = iota // always FixedUSD
	SizingProportional                   // Proportion * whale notional
)

// Policy is the strategy configuration.
type Policy struct {
	Sizing          SizingMode
	FixedUSD        float64
	Proportion      float64 // proportional: fraction of the whale's notional
	MinSizeUSD      float64 // skip proposals below this
	MaxSizeUSD      float64 // clamp proposals to this (>0)
	MaxSlippage     float64 // price units, e.g. 0.03 = 3 cents
	MinLiquidityUSD float64
}

// Market is the live context for the traded token (from marketdata).
type Market struct {
	CurrentPrice float64
	LiquidityUSD float64
	ConditionID  string
	Active       bool
	Allowed      bool // passed the allowlist (decided by the caller)
}

// Outcome is the decision result.
type Outcome struct {
	Propose  bool
	Reason   string
	Proposal bus.OrderProposal
}

func skip(reason string) Outcome { return Outcome{Propose: false, Reason: reason} }

// Decide applies filters, the slippage guard, and sizing to produce a proposal
// or a skip. Order matters: cheap filters first, sizing last.
func Decide(t bus.TradeDetected, m Market, p Policy) Outcome {
	whalePrice, err := strconv.ParseFloat(t.Price, 64)
	if err != nil {
		return skip("invalid trade price")
	}
	whaleSize, err := strconv.ParseFloat(t.Size, 64)
	if err != nil {
		return skip("invalid trade size")
	}
	if whalePrice <= 0 || whaleSize <= 0 {
		return skip("non-positive trade price or size")
	}
	if t.Side != "buy" && t.Side != "sell" {
		return skip("unknown side")
	}
	if !m.Active {
		return skip("market inactive or closed")
	}
	if !m.Allowed {
		return skip("market not in allowlist")
	}
	if m.LiquidityUSD < p.MinLiquidityUSD {
		return skip(fmt.Sprintf("insufficient liquidity ($%.0f < $%.0f)", m.LiquidityUSD, p.MinLiquidityUSD))
	}
	if !WithinSlippage(t.Side, whalePrice, m.CurrentPrice, p.MaxSlippage) {
		return skip("price moved beyond max slippage")
	}

	sizeUSD, err := SizeUSD(t, p)
	if err != nil {
		return skip("sizing failed: " + err.Error())
	}
	if sizeUSD < p.MinSizeUSD {
		return skip(fmt.Sprintf("size $%.2f below min $%.2f", sizeUSD, p.MinSizeUSD))
	}

	limit, ok := limitPrice(t.Side, whalePrice, p.MaxSlippage)
	if !ok {
		// The slippage allowance pushed the limit outside the tradable (0,1)
		// range — refuse rather than emit a limit that ignores the policy.
		return skip("limit price out of tradable range")
	}

	return Outcome{
		Propose: true,
		Reason:  "ok",
		Proposal: bus.OrderProposal{
			ID:          ProposalID(t),
			SourceTrade: t,
			TokenID:     t.TokenID,
			Side:        t.Side,
			LimitPrice:  formatPrice(limit),
			SizeUSD:     sizeUSD,
			Reason:      "mirror whale " + shortWallet(t.Wallet),
		},
	}
}

// SizeUSD computes the proposal size in USD per the policy, clamped to MaxSizeUSD.
func SizeUSD(t bus.TradeDetected, p Policy) (float64, error) {
	var size float64
	switch p.Sizing {
	case SizingFixed:
		size = p.FixedUSD
	case SizingProportional:
		whalePrice, err := strconv.ParseFloat(t.Price, 64)
		if err != nil {
			return 0, fmt.Errorf("price: %w", err)
		}
		whaleSize, err := strconv.ParseFloat(t.Size, 64)
		if err != nil {
			return 0, fmt.Errorf("size: %w", err)
		}
		size = whaleSize * whalePrice * p.Proportion
	default:
		return 0, fmt.Errorf("unknown sizing mode %d", p.Sizing)
	}
	if p.MaxSizeUSD > 0 && size > p.MaxSizeUSD {
		size = p.MaxSizeUSD
	}
	return size, nil
}

// WithinSlippage reports whether the current price is still acceptable versus
// the whale's fill price: for a buy the price must not have risen more than
// maxSlippage; for a sell it must not have fallen more than maxSlippage.
func WithinSlippage(side string, whalePrice, currentPrice, maxSlippage float64) bool {
	switch side {
	case "buy":
		return currentPrice <= whalePrice+maxSlippage+epsilon
	case "sell":
		return currentPrice >= whalePrice-maxSlippage-epsilon
	default:
		return false
	}
}

// epsilon makes the slippage boundary inclusive despite float rounding. It is
// far smaller than the price tick (~0.001), so it can never admit a trade that
// is a meaningful cent over the threshold.
const epsilon = 1e-9

// limitPrice is the worst price we'll accept, derived from the whale price plus
// the slippage allowance, rounded to the price tick. ok=false if that limit
// falls outside the tradable (0,1) range, in which case the proposal is skipped
// rather than emitting a clamped limit that no longer reflects the policy.
func limitPrice(side string, whalePrice, maxSlippage float64) (float64, bool) {
	var v float64
	if side == "buy" {
		v = whalePrice + maxSlippage
	} else {
		v = whalePrice - maxSlippage
	}
	v = math.Round(v*1000) / 1000 // Polymarket ticks are sub-cent; 3dp is plenty
	if v < 0.001 || v > 0.999 {
		return 0, false
	}
	return v, true
}

func formatPrice(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// ProposalID is the deterministic, unique id for the proposal mirroring a source
// trade. It includes the wallet because one OrderFilled log can produce trades
// for both the maker and the taker at the same (txHash, logIndex).
func ProposalID(t bus.TradeDetected) string {
	return fmt.Sprintf("prop-%s-%d-%s", t.TxHash, t.LogIndex, t.Wallet)
}

func shortWallet(w string) string {
	if len(w) <= 12 {
		return w
	}
	return w[:6] + "…" + w[len(w)-4:]
}
