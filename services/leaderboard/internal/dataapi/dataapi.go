// Package dataapi reads per-wallet trading history and portfolio figures from
// the Polymarket data-api host. It feeds the consistency-stats pipeline:
//
//	GET /activity?user=<addr>&limit=500&offset=<n> -> [{type, side, usdcSize, conditionId, ...}]
//	GET /value?user=<addr>                         -> [{"user","value"}]
//	GET /traded?user=<addr>                        -> {"user","traded"}
//
// Like internal/client it uses a 15s timeout, a custom User-Agent, and a
// bounded retry/backoff on transient failures (network error, HTTP 429/5xx).
package dataapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
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

		body, err := c.fetchBody(ctx, endpoint, "/activity")
		if err != nil {
			return nil, err
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

	body, err := c.fetchBody(ctx, endpoint, "/value")
	if err != nil {
		return 0, err
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

// tradedResp is the /traded response object.
type tradedResp struct {
	User   string `json:"user"`
	Traded int    `json:"traded"`
}

// Traded returns the wallet's lifetime traded-market count.
func (c *Client) Traded(ctx context.Context, wallet string) (int, error) {
	q := url.Values{}
	q.Set("user", wallet)
	endpoint := c.baseURL + "/traded?" + q.Encode()

	body, err := c.fetchBody(ctx, endpoint, "/traded")
	if err != nil {
		return 0, err
	}
	var resp tradedResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("decode traded: %w", err)
	}
	return resp.Traded, nil
}

// retry tuning. Kept short so tests run fast.
const (
	maxAttempts   = 3
	errBodyMaxLen = 256
)

var backoffSchedule = []time.Duration{100 * time.Millisecond, 200 * time.Millisecond}

// fetchBody performs the HTTP GET with a bounded retry/backoff loop. It retries
// on network errors and on transient HTTP statuses (429 or >=500), up to
// maxAttempts total. Other non-200 statuses fail immediately.
func (c *Client) fetchBody(ctx context.Context, endpoint, path string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			d := backoffSchedule[len(backoffSchedule)-1]
			if attempt-1 < len(backoffSchedule) {
				d = backoffSchedule[attempt-1]
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(d):
			}
		}

		body, transient, err := c.doOnce(ctx, endpoint, path)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !transient {
			return nil, err
		}
	}
	return nil, fmt.Errorf("data-api %s: giving up after %d attempts: %w", path, maxAttempts, lastErr)
}

// doOnce issues a single request. transient=true marks failures worth retrying
// (network error, or HTTP 429 / >=500).
func (c *Client) doOnce(ctx context.Context, endpoint, path string) (body []byte, transient bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("fetch %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err = io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, true, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		isTransient := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return nil, isTransient, fmt.Errorf("data-api %s: status %d: %s", path, resp.StatusCode, truncate(body, errBodyMaxLen))
	}
	return body, false, nil
}

// truncate bounds a response body for safe inclusion in error messages.
func truncate(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + fmt.Sprintf("... (%d bytes truncated)", len(b)-max)
}
