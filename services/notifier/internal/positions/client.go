// Package positions tracks the user's own open Polymarket positions (read-only,
// public data-api) so the notifier can flag when a tracked whale trades a market
// the user holds — e.g. a whale exiting an outcome you're in.
package positions

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/JacobTDang/LobsterRoll/pkg/dataapi"
)

const userAgent = "lobsterroll-notifier/1.0"

// Position is one open position from data-api /positions.
type Position struct {
	Asset         string  `json:"asset"`         // the held outcome's tokenId
	ConditionID   string  `json:"conditionId"`   // market id
	OppositeAsset string  `json:"oppositeAsset"` // the complementary outcome's tokenId
	Outcome       string  `json:"outcome"`       // e.g. "Yes"
	Size          float64 `json:"size"`          // shares held
	AvgPrice      float64 `json:"avgPrice"`
	CurPrice      float64 `json:"curPrice"`
	CurrentValue  float64 `json:"currentValue"`
	Redeemable    bool    `json:"redeemable"` // resolved & claimable
	Title         string  `json:"title"`
	Slug          string  `json:"slug"`
}

// Client reads a wallet's positions over HTTP via the shared data-api client.
type Client struct {
	api *dataapi.Client
}

// New returns a Client. An empty baseURL uses the shared default host; a nil hc
// uses a default-timeout client.
func New(baseURL string, hc *http.Client) *Client {
	return &Client{api: dataapi.New(baseURL, userAgent, hc)}
}

// Fetch returns the open positions for wallet (public, read-only).
func (c *Client) Fetch(ctx context.Context, wallet string) ([]Position, error) {
	q := url.Values{}
	q.Set("user", wallet)
	q.Set("sizeThreshold", "1")
	var out []Position
	if err := c.api.GetJSON(ctx, "/positions", q, &out); err != nil {
		return nil, fmt.Errorf("positions %s: %w", wallet, err)
	}
	return out, nil
}
