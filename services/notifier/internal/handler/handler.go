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

// Sender delivers a message. *telegram.Client satisfies it.
type Sender interface {
	Send(ctx context.Context, chatID, text string) error
}

// Handler enriches and sends alerts.
type Handler struct {
	enrich Enricher
	sender Sender
	chatID string
	log    *slog.Logger
}

// New constructs a Handler.
func New(enrich Enricher, sender Sender, chatID string, log *slog.Logger) *Handler {
	return &Handler{enrich: enrich, sender: sender, chatID: chatID, log: log}
}

// Handle enriches td, formats an alert, and sends it. Errors are logged, not
// returned, so one bad trade can't stall the consumer.
func (h *Handler) Handle(ctx context.Context, td bus.TradeDetected) {
	market := format.Market{Found: false}
	if td.TokenID != "" {
		resp, err := h.enrich.EnrichToken(ctx, &lobsterrollv1.EnrichTokenRequest{TokenId: td.TokenID})
		switch {
		case err == nil:
			market = format.Market{Question: resp.GetMarketQuestion(), Outcome: resp.GetOutcome(), Slug: resp.GetMarketSlug(), Found: true}
		case status.Code(err) == codes.NotFound:
			// Genuinely unknown token — alert as "Unknown market".
		default:
			// Transient failure (enrichment down, timeout): don't mislabel as unknown.
			h.log.Warn("enrichment lookup failed; alerting without market", "token", td.TokenID, "err", err)
			market = format.Market{LookupFailed: true}
		}
	}

	text := format.FormatAlert(td, market)
	if err := h.sender.Send(ctx, h.chatID, text); err != nil {
		h.log.Error("send alert failed", "wallet", td.Wallet, "tx", td.TxHash, "err", err)
		return
	}
	h.log.Info("alert sent", "wallet", td.Wallet, "side", td.Side, "size", td.Size)
}
