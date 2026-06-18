// Package handler turns a detected trade into an enriched, formatted Telegram
// alert. It never blocks the bus: enrichment failures degrade gracefully to an
// "unknown market" alert, and send failures are logged.
package handler

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/pkg/bus"
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
	enrich Enricher
	stats  WhaleStatsLookuper
	sender Sender
	chatID string
	log    *slog.Logger
}

// New constructs a Handler. stats may be nil to disable whale track-record
// enrichment (alerts then render without the stats line).
func New(enrich Enricher, stats WhaleStatsLookuper, sender Sender, chatID string, log *slog.Logger) *Handler {
	return &Handler{enrich: enrich, stats: stats, sender: sender, chatID: chatID, log: log}
}

// Handle enriches td, formats an alert, and sends it. Errors are logged, not
// returned, so one bad trade can't stall the consumer.
func (h *Handler) Handle(ctx context.Context, td bus.TradeDetected) {
	market := h.resolveMarket(ctx, td.TokenID)

	// Best-effort whale track record: a lookup failure or unknown wallet just
	// omits the stats line — it must never block or fail the alert.
	ws := h.lookupStats(ctx, td.Wallet)

	text := format.FormatAlert(td, market, ws)
	if err := h.sender.Send(ctx, h.chatID, text); err != nil {
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
		return format.Market{Question: resp.GetMarketQuestion(), Outcome: resp.GetOutcome(), Slug: resp.GetMarketSlug(), Found: true}
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

// HandleConsensus enriches the consensus token and sends the premium alert.
// Like Handle, it degrades gracefully and never returns errors to the bus.
func (h *Handler) HandleConsensus(ctx context.Context, sig bus.ConsensusSignal) {
	market := h.resolveMarket(ctx, sig.TokenID)

	text := format.FormatConsensus(sig, market)
	if err := h.sender.Send(ctx, h.chatID, text); err != nil {
		h.log.Error("send consensus alert failed", "token", sig.TokenID, "count", sig.Count, "err", err)
		return
	}
	h.log.Info("consensus alert sent", "token", sig.TokenID, "side", sig.Side, "count", sig.Count)
}
