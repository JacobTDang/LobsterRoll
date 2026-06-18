// Package client reads live market midprices from the Polymarket CLOB. We poll
// /midpoint periodically (rather than subscribing to the websocket) — for a
// closing-line comparison we only need periodic mids, and polling is far more
// robust than the CLOB ws (which is known to silently freeze) and fully testable.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/httpx"
)

// DefaultBaseURL is the public Polymarket CLOB host.
const DefaultBaseURL = "https://clob.polymarket.com"

const userAgent = "lobsterroll-pricewatch/1.0"

// Client fetches midprices over HTTP.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a Client. If hc is nil a 10s-timeout client is used.
func New(baseURL string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{baseURL: baseURL, http: hc}
}

// Midpoint returns the current midprice (0..1) for tokenID. The CLOB returns
// {"mid":"0.52"} with the price as a string.
func (c *Client) Midpoint(ctx context.Context, tokenID string) (float64, error) {
	q := url.Values{}
	q.Set("token_id", tokenID)
	body, err := httpx.Get(ctx, c.http, c.baseURL+"/midpoint?"+q.Encode(), userAgent, 1<<16)
	if err != nil {
		return 0, fmt.Errorf("midpoint %s: %w", tokenID, err)
	}
	var resp struct {
		Mid string `json:"mid"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("decode midpoint %s: %w", tokenID, err)
	}
	mid, err := strconv.ParseFloat(resp.Mid, 64)
	if err != nil {
		return 0, fmt.Errorf("parse mid %q for %s: %w", resp.Mid, tokenID, err)
	}
	// A share price must be a real number in [0,1]; anything else is junk that
	// would silently corrupt a later CLV computation if stored.
	if math.IsNaN(mid) || math.IsInf(mid, 0) || mid < 0 || mid > 1 {
		return 0, fmt.Errorf("midpoint %s out of range: %v", tokenID, mid)
	}
	return mid, nil
}
