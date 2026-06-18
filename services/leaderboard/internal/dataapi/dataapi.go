// Package dataapi reads per-wallet trading history and portfolio figures from
// the Polymarket data-api host. It feeds the consistency-stats pipeline:
//
//	GET /activity?user=<addr>&limit=500&offset=<n> -> [{type, side, size, usdcSize, conditionId, ...}]
//	GET /value?user=<addr>                         -> [{"user","value"}]
//
// HTTP (15s timeout, custom UA, bounded retry/backoff on 429/5xx) is shared via
// pkg/httpx.
package dataapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/httpx"
)

// DefaultBaseURL is the public Polymarket data-api host.
const DefaultBaseURL = "https://data-api.polymarket.com"

// userAgent identifies us to upstream WAFs (the default Go UA gets blocked).
const userAgent = "lobsterroll-leaderboard/1.0"

// activityPageSize is the per-request /activity page size the host accepts.
const activityPageSize = 500

// Activity is one row from /activity. We only consume the fields the win-rate
// algorithm needs; unknown fields are ignored.
type Activity struct {
	Type        string  `json:"type"`        // TRADE, REDEEM, MERGE, SPLIT, REWARD, ...
	Side        string  `json:"side"`        // BUY or SELL (TRADE only)
	Size        float64 `json:"size"`        // share quantity of the event
	USDCSize    float64 `json:"usdcSize"`    // cash size of the event
	ConditionID string  `json:"conditionId"` // market identifier
}

// Client reads per-wallet data over HTTP.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a Client. If hc is nil, a Client with a 15s timeout is used.
func New(baseURL string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: baseURL, http: hc}
}

// Activity returns up to maxRows activity rows for wallet, paginating by offset
// in pages of activityPageSize until a short (final) page or maxRows is reached.
// A non-positive maxRows returns no rows.
func (c *Client) Activity(ctx context.Context, wallet string, maxRows int) ([]Activity, error) {
	if maxRows <= 0 {
		return nil, nil
	}
	var all []Activity
	for offset := 0; len(all) < maxRows; offset += activityPageSize {
		q := url.Values{}
		q.Set("user", wallet)
		q.Set("limit", strconv.Itoa(activityPageSize))
		q.Set("offset", strconv.Itoa(offset))
		endpoint := c.baseURL + "/activity?" + q.Encode()

		body, err := httpx.Get(ctx, c.http, endpoint, userAgent, 8<<20)
		if err != nil {
			return nil, fmt.Errorf("data-api /activity: %w", err)
		}
		var page []Activity
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decode activity: %w", err)
		}
		all = append(all, page...)
		// A short page (fewer than a full page) means we've reached the end.
		if len(page) < activityPageSize {
			break
		}
	}
	if len(all) > maxRows {
		all = all[:maxRows]
	}
	return all, nil
}

// valueRow is one element of the /value response array.
type valueRow struct {
	User  string  `json:"user"`
	Value float64 `json:"value"`
}

// Value returns the wallet's current portfolio value in USD. An empty response
// (no portfolio) yields 0 with no error.
func (c *Client) Value(ctx context.Context, wallet string) (float64, error) {
	q := url.Values{}
	q.Set("user", wallet)
	endpoint := c.baseURL + "/value?" + q.Encode()

	body, err := httpx.Get(ctx, c.http, endpoint, userAgent, 8<<20)
	if err != nil {
		return 0, fmt.Errorf("data-api /value: %w", err)
	}
	var rows []valueRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return 0, fmt.Errorf("decode value: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rows[0].Value, nil
}

