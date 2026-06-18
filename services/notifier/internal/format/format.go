// Package format renders a detected trade into a human-readable Telegram alert.
package format

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
)

// Market is the resolved market context for a trade (from enrichment-svc).
type Market struct {
	Question string
	Outcome  string
	Found    bool
	// LookupFailed distinguishes a transient enrichment failure (couldn't look
	// up) from a genuinely unknown token, so the alert isn't mislabeled.
	LookupFailed bool
}

// FormatAlert renders a one-way alert for a detected trade.
func FormatAlert(td bus.TradeDetected, m Market) string {
	emoji, label := "🔴", "SELL"
	if strings.EqualFold(td.Side, "buy") {
		emoji, label = "🟢", "BUY"
	}

	var market string
	switch {
	case m.Found:
		market = fmt.Sprintf("%s — %s", m.Question, m.Outcome)
	case m.LookupFailed:
		market = fmt.Sprintf("Market lookup unavailable (token %s)", shortenMiddle(td.TokenID, 4, 4))
	default:
		market = fmt.Sprintf("Unknown market (token %s)", shortenMiddle(td.TokenID, 4, 4))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s %s  whale %s\n", emoji, label, shortenHex(td.Wallet))
	fmt.Fprintf(&b, "%s\n", market)
	fmt.Fprintf(&b, "%s shares @ $%s  (≈ $%s)\n", td.Size, td.Price, notional(td.Size, td.Price))
	fmt.Fprintf(&b, "https://polygonscan.com/tx/%s", td.TxHash)
	return b.String()
}

// FormatProposal renders an order proposal awaiting approval.
func FormatProposal(p bus.OrderProposal) string {
	label := "SELL"
	if strings.EqualFold(p.Side, "buy") {
		label = "BUY"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "📋 Mirror %s  $%.2f @ ≤ $%s\n", label, p.SizeUSD, p.LimitPrice)
	fmt.Fprintf(&b, "token %s\n", shortenMiddle(p.TokenID, 4, 4))
	fmt.Fprintf(&b, "whale %s filled %s @ $%s\n", shortenHex(p.SourceTrade.Wallet), p.SourceTrade.Size, p.SourceTrade.Price)
	b.WriteString("Approve?")
	return b.String()
}

// notional returns size*price formatted to 2 decimals (USDC); "?" if unparsable.
func notional(size, price string) string {
	s, ok1 := new(big.Rat).SetString(size)
	p, ok2 := new(big.Rat).SetString(price)
	if !ok1 || !ok2 {
		return "?"
	}
	return new(big.Rat).Mul(s, p).FloatString(2)
}

// shortenHex renders 0x-addresses as 0xabcd…wxyz.
func shortenHex(addr string) string {
	if len(addr) <= 12 || !strings.HasPrefix(addr, "0x") {
		return addr
	}
	return addr[:6] + "…" + addr[len(addr)-4:]
}

// shortenMiddle keeps the first head and last tail runes of a long id.
func shortenMiddle(s string, head, tail int) string {
	r := []rune(s)
	if len(r) <= head+tail+1 {
		return s
	}
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}
