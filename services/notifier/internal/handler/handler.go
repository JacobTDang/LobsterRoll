// Package handler turns a detected trade into an enriched, formatted Telegram
// alert. It never blocks the bus: enrichment failures degrade gracefully to an
// "unknown market" alert, and send failures are logged.
package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/dedup"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/format"
)

// Enricher resolves a tokenId to market context. *lobsterrollv1.EnrichmentClient
// satisfies it.
type Enricher interface {
	EnrichToken(ctx context.Context, in *lobsterrollv1.EnrichTokenRequest, opts ...grpc.CallOption) (*lobsterrollv1.EnrichTokenResponse, error)
}

// WhaleStatsLookuper resolves a wallet's leaderboard track record. The generated
// lobsterrollv1.LeaderboardClient satisfies it. May be nil to disable enrichment.
type WhaleStatsLookuper interface {
	GetWalletStats(ctx context.Context, in *lobsterrollv1.GetWalletStatsRequest, opts ...grpc.CallOption) (*lobsterrollv1.WalletStats, error)
}

// Sender delivers a message. *telegram.Client satisfies it.
type Sender interface {
	Send(ctx context.Context, chatID, text string) error
}

// Handler enriches and sends alerts.
type Handler struct {
	enrich   Enricher
	stats    WhaleStatsLookuper
	sender   Sender
	chatID   string
	dedup    *dedup.TTLSet // exact-trade dedup: suppresses re-emits of the SAME fill
	cooldown *dedup.TTLSet // burst cooldown: collapses repeated wallet+market+side trades
	now      func() time.Time
	log      *slog.Logger
}

// New constructs a Handler. stats may be nil to disable whale track-record
// enrichment (alerts then render without the stats line). dd dedups exact
// re-emitted trades; cooldown (may be nil) collapses a whale's repeated trades
// on the same market+side into one alert per cooldown window.
func New(enrich Enricher, stats WhaleStatsLookuper, sender Sender, chatID string, dd, cooldown *dedup.TTLSet, log *slog.Logger) *Handler {
	return &Handler{enrich: enrich, stats: stats, sender: sender, chatID: chatID, dedup: dd, cooldown: cooldown, now: time.Now, log: log}
}

// tradeKey uniquely identifies a detected trade. The on-chain (tx, logIndex)
// pins the exact fill; wallet/token/side guard against any aggregation reuse.
// Two legs of one tx (a rotation: sell A, buy B) differ by logIndex/token/side
// and are NOT deduped — only a true re-emit of the same fill is.
func tradeKey(td bus.TradeDetected) string {
	return fmt.Sprintf("%s:%d:%s:%s:%s", td.TxHash, td.LogIndex,
		strings.ToLower(td.Wallet), td.TokenID, strings.ToLower(td.Side))
}

// cooldownKey ignores the specific tx: it groups a wallet's repeated trades on
// the same market + side, so scaling into a position (many small fills across
// separate txs) yields one alert per cooldown window instead of a burst.
func cooldownKey(td bus.TradeDetected) string {
	return fmt.Sprintf("cd:%s:%s:%s", strings.ToLower(td.Wallet), td.TokenID, strings.ToLower(td.Side))
}

// Handle enriches td, formats an alert, and sends it (once). Exact re-emits are
// dropped; repeated wallet+market+side trades within the cooldown are collapsed.
// Errors are logged, not returned, so one bad trade can't stall the consumer.
func (h *Handler) Handle(ctx context.Context, td bus.TradeDetected) {
	key := tradeKey(td)
	if h.dedup != nil && !h.dedup.Add(key) {
		h.log.Info("duplicate trade alert suppressed", "wallet", td.Wallet, "tx", td.TxHash)
		return
	}
	if h.cooldown != nil && !h.cooldown.Add(cooldownKey(td)) {
		h.log.Info("alert cooled down (repeat on same market+side)", "wallet", td.Wallet, "token", td.TokenID, "side", td.Side)
		return
	}

	market := h.resolveMarket(ctx, td.TokenID)

	// Drop trades on games that are already over — they're not actionable. Only
	// filter when we actually know the end time (Found + EndDateUnix > 0); an
	// unknown/unresolved market still alerts.
	if market.Found && market.EndDateUnix > 0 && h.now().Unix() > market.EndDateUnix {
		h.log.Info("skipping alert; game already ended", "wallet", td.Wallet, "token", td.TokenID, "end", market.EndDateUnix)
		return
	}

	// Best-effort whale track record: a lookup failure or unknown wallet just
	// omits the stats line — it must never block or fail the alert.
	ws := h.lookupStats(ctx, td.Wallet)

	text := format.FormatAlert(td, market, ws)
	if err := h.sender.Send(ctx, h.chatID, text); err != nil {
		// A failed send delivered nothing, so undo both caches. Removing the exact
		// dedup key lets this fill retry on re-emit; the cooldown key MUST also be
		// cleared or it would block that very retry (it's keyed per market+side,
		// not per fill). Net effect: still one alert per burst — just the first
		// one that actually sends — never zero because of a transient failure.
		if h.dedup != nil {
			h.dedup.Remove(key)
		}
		if h.cooldown != nil {
			h.cooldown.Remove(cooldownKey(td))
		}
		h.log.Error("send alert failed", "wallet", td.Wallet, "tx", td.TxHash, "err", err)
		return
	}
	h.log.Info("alert sent", "wallet", td.Wallet, "side", td.Side, "size", td.Size)
}

// resolveMarket enriches a tokenId to market context, degrading gracefully: an
// empty token or a NotFound yields an unresolved Market ("Unknown market"); a
// transient enrichment error yields LookupFailed (so it isn't mislabeled unknown).
func (h *Handler) resolveMarket(ctx context.Context, tokenID string) format.Market {
	if tokenID == "" {
		return format.Market{}
	}
	resp, err := h.enrich.EnrichToken(ctx, &lobsterrollv1.EnrichTokenRequest{TokenId: tokenID})
	switch {
	case err == nil:
		return format.Market{Question: resp.GetMarketQuestion(), Outcome: resp.GetOutcome(), Slug: resp.GetMarketSlug(), Found: true, EndDateUnix: resp.GetEndDateUnix()}
	case status.Code(err) == codes.NotFound:
		return format.Market{}
	default:
		h.log.Warn("enrichment lookup failed; alerting without market", "token", tokenID, "err", err)
		return format.Market{LookupFailed: true}
	}
}

// lookupStats best-effort fetches the whale's leaderboard track record. It
// returns a zero (OK=false) WhaleStats on any miss so the caller renders no
// stats line.
func (h *Handler) lookupStats(ctx context.Context, wallet string) format.WhaleStats {
	if h.stats == nil || wallet == "" {
		return format.WhaleStats{}
	}
	resp, err := h.stats.GetWalletStats(ctx, &lobsterrollv1.GetWalletStatsRequest{Wallet: wallet})
	if err != nil {
		h.log.Warn("wallet stats lookup failed; alerting without track record", "wallet", wallet, "err", err)
		return format.WhaleStats{}
	}
	if !resp.GetFound() {
		return format.WhaleStats{}
	}
	return format.WhaleStats{
		WinRate:         resp.GetWinRate(),
		ResolvedMarkets: int(resp.GetResolvedMarkets()),
		RealizedPnlUSD:  resp.GetRealizedPnl(),
		PortfolioUSD:    resp.GetPortfolioValue(),
		OK:              true,
	}
}

// consensusKey identifies a consensus signal by its exact cohort, so a NATS
// redelivery of the same signal is suppressed while a genuinely different cohort
// (a re-fire after dissipation, or growth to a new size) still alerts. Wallets
// arrive distinct + sorted from the window, so the join is stable.
func consensusKey(sig bus.ConsensusSignal) string {
	return fmt.Sprintf("consensus:%s:%s:%s", sig.TokenID, strings.ToLower(sig.Side), strings.Join(sig.Wallets, ","))
}

// HandleConsensus enriches the consensus token and sends the premium alert.
// Like Handle, it degrades gracefully and never returns errors to the bus.
func (h *Handler) HandleConsensus(ctx context.Context, sig bus.ConsensusSignal) {
	key := consensusKey(sig)
	if h.dedup != nil && !h.dedup.Add(key) {
		h.log.Info("duplicate consensus alert suppressed", "token", sig.TokenID, "count", sig.Count)
		return
	}

	market := h.resolveMarket(ctx, sig.TokenID)

	text := format.FormatConsensus(sig, market)
	if err := h.sender.Send(ctx, h.chatID, text); err != nil {
		if h.dedup != nil {
			h.dedup.Remove(key) // failed send delivered nothing -> allow retry
		}
		h.log.Error("send consensus alert failed", "token", sig.TokenID, "count", sig.Count, "err", err)
		return
	}
	h.log.Info("consensus alert sent", "token", sig.TokenID, "side", sig.Side, "count", sig.Count)
}
