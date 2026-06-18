// Package format renders a detected trade into a human-readable Telegram alert.
package format

import (
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
)

// Market is the resolved market context for a trade (from enrichment-svc).
type Market struct {
	Question string
	Outcome  string
	Slug     string // gamma market slug → polymarket.com/event/<slug>
	Found    bool
	// LookupFailed distinguishes a transient enrichment failure (couldn't look
	// up) from a genuinely unknown token, so the alert isn't mislabeled.
	LookupFailed bool
	// EndDateUnix is when the market/game ends (unix secs); 0 if unknown.
	EndDateUnix int64
}

// WhaleStats is the resolved track record for a whale (from the leaderboard
// cache). OK is false when no stats were available; the line is then omitted.
type WhaleStats struct {
	WinRate         float64 // 0..1
	ResolvedMarkets int
	RealizedPnlUSD  float64
	PortfolioUSD    float64
	OK              bool
}

// FormatAlert renders a one-way alert for a detected trade: what the whale is
// betting on, their track record (when available), how much (USD first),
// whether they're entering or exiting, and when.
func FormatAlert(td bus.TradeDetected, m Market, ws WhaleStats) string {
	// A buy opens/adds to a position (ENTER); a sell closes/reduces it (EXIT).
	emoji, action, side := "🔴", "EXIT", "SELL"
	if strings.EqualFold(td.Side, "buy") {
		emoji, action, side = "🟢", "ENTER", "BUY"
	}

	lines := []string{fmt.Sprintf("%s %s (%s)  whale %s", emoji, action, side, shortenHex(td.Wallet))}
	lines = append(lines, marketLine(td, m))
	if m.Found && m.EndDateUnix > 0 {
		lines = append(lines, "🏁 game "+time.Unix(m.EndDateUnix, 0).UTC().Format("2006-01-02 15:04 UTC"))
	}
	if ws.OK {
		lines = append(lines, fmt.Sprintf("👤 %d%% win (%d mkts) · realized %s · %s portfolio",
			int(ws.WinRate*100+0.5), ws.ResolvedMarkets, signedMoney(ws.RealizedPnlUSD), abbrevMoney(ws.PortfolioUSD)))
	}
	lines = append(lines, fmt.Sprintf("💵 $%s  ·  %s @ $%s", notional(td.Size, td.Price), td.Size, td.Price))
	if !td.ObservedAt.IsZero() {
		lines = append(lines, fmt.Sprintf("🕒 %s", td.ObservedAt.UTC().Format("2006-01-02 15:04 UTC")))
	}
	if m.Found && m.Slug != "" {
		lines = append(lines, "📊 https://polymarket.com/event/"+m.Slug)
	}
	return strings.Join(lines, "\n")
}

// marketLine renders the "what they're betting on" line, degrading gracefully.
func marketLine(td bus.TradeDetected, m Market) string {
	switch {
	case m.Found:
		return fmt.Sprintf("%s → %s", m.Question, m.Outcome)
	case m.LookupFailed:
		return fmt.Sprintf("Market lookup unavailable (token %s)", shortenMiddle(td.TokenID, 4, 4))
	default:
		return fmt.Sprintf("Unknown market (token %s)", shortenMiddle(td.TokenID, 4, 4))
	}
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

// FormatConsensus renders the premium alert fired when multiple tracked wallets
// converge on the same outcome token and side within a rolling window.
func FormatConsensus(sig bus.ConsensusSignal, m Market) string {
	side := "SELL"
	if strings.EqualFold(sig.Side, "buy") {
		side = "BUY"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "🔥 CONSENSUS — %d tracked wallets %s\n", sig.Count, side)

	switch {
	case m.Found:
		fmt.Fprintf(&b, "%s → %s\n", m.Question, m.Outcome)
	case m.LookupFailed:
		fmt.Fprintf(&b, "Market lookup unavailable (token %s)\n", shortenMiddle(sig.TokenID, 4, 4))
	default:
		fmt.Fprintf(&b, "Unknown market (token %s)\n", shortenMiddle(sig.TokenID, 4, 4))
	}

	fmt.Fprintf(&b, "%d wallets · combined %s · %s window", sig.Count, abbrevMoney(sig.CombinedUSD), humanWindow(sig.WindowSecs))
	if m.Found && m.Slug != "" {
		fmt.Fprintf(&b, "\n📊 https://polymarket.com/event/%s", m.Slug)
	}
	return b.String()
}

// abbrevMoney renders a non-negative USD magnitude compactly: $45, $1.2k,
// $31.0M. The sign is dropped — callers that need it use signedMoney.
func abbrevMoney(v float64) string {
	if v < 0 {
		v = -v
	}
	switch {
	case v >= 1_000_000:
		return fmt.Sprintf("$%.1fM", v/1_000_000)
	case v >= 1_000:
		return fmt.Sprintf("$%.1fk", v/1_000)
	default:
		return fmt.Sprintf("$%.0f", v)
	}
}

// signedMoney prefixes abbrevMoney with an explicit sign for positive/negative
// values; zero is rendered as "$0" without a sign.
func signedMoney(v float64) string {
	switch {
	case v > 0:
		return "+" + abbrevMoney(v)
	case v < 0:
		return "-" + abbrevMoney(v)
	default:
		return "$0"
	}
}

// humanWindow renders a duration in seconds as a compact window like 30m or 6h.
func humanWindow(secs int) string {
	if secs >= 3600 {
		return fmt.Sprintf("%dh", secs/3600)
	}
	return fmt.Sprintf("%dm", secs/60)
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
