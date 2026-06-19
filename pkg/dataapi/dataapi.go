// Package dataapi is a shared HTTP client for the public Polymarket data-api
// host (https://data-api.polymarket.com). Services build typed wrappers —
// per-wallet activity/value (leaderboard), open positions (notifier) — on top of
// GetJSON, so the host, the WAF-friendly request, and the decode live in one place.
package dataapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/httpx"
)

// BaseURL is the public Polymarket data-api host.
const BaseURL = "https://data-api.polymarket.com"

// maxBody caps a response body (8 MiB) — generous for paginated activity.
const maxBody = 8 << 20

// Client is a thin GET+decode client for the data-api host.
type Client struct {
	baseURL   string
	userAgent string
	http      *http.Client
}

// New returns a Client. An empty baseURL uses BaseURL; a nil hc uses a 15s
// client. userAgent identifies the caller to upstream WAFs (the default Go UA is
// blocked).
func New(baseURL, userAgent string, hc *http.Client) *Client {
	if baseURL == "" {
		baseURL = BaseURL
	}
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: baseURL, userAgent: userAgent, http: hc}
}

// GetJSON fetches baseURL+path (with optional query) and decodes the JSON body
// into out. The shared retry/backoff/UA handling lives in pkg/httpx.
func (c *Client) GetJSON(ctx context.Context, path string, q url.Values, out any) error {
	endpoint := c.baseURL + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}
	body, err := httpx.Get(ctx, c.http, endpoint, c.userAgent, maxBody)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}
