// Package positions tracks the user's own open Polymarket positions (read-only,
// public data-api) so the notifier can flag when a tracked whale trades a market
// the user holds — e.g. a whale exiting an outcome you're in.
package positions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/httpx"
)

// DefaultBaseURL is the public Polymarket data-api host.
const DefaultBaseURL = "https://data-api.polymarket.com"

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

// Client reads a wallet's positions over HTTP.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a Client. An empty baseURL uses DefaultBaseURL; a nil hc uses a
// 10s-timeout client.
func New(baseURL string, hc *http.Client) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{baseURL: baseURL, http: hc}
}

// Fetch returns the open positions for wallet (public, read-only).
func (c *Client) Fetch(ctx context.Context, wallet string) ([]Position, error) {
	q := url.Values{}
	q.Set("user", wallet)
	q.Set("sizeThreshold", "1")
	body, err := httpx.Get(ctx, c.http, c.baseURL+"/positions?"+q.Encode(), userAgent, 8<<20)
	if err != nil {
		return nil, fmt.Errorf("positions %s: %w", wallet, err)
	}
	var out []Position
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode positions %s: %w", wallet, err)
	}
	return out, nil
}
