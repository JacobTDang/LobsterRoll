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
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	pmapi "github.com/JacobTDang/LobsterRoll/pkg/dataapi"
	"github.com/JacobTDang/LobsterRoll/pkg/metrics"
)

// userAgent identifies us to upstream WAFs (the default Go UA gets blocked).
const userAgent = "lobsterroll-leaderboard/1.0"

// activityPageSize is the per-request /activity page size the host accepts.
const activityPageSize = 500

// mActivityCapped counts crawls that hit the maxRows safety ceiling — i.e. the
// wallet has MORE history than we fetched, so its stats are computed on a partial
// record. Surfaced so that truncation (and any resulting skew) is observable.
var mActivityCapped = metrics.NewCounter("lobsterroll_leaderboard_activity_capped_total", "wallet activity crawls that hit the maxRows ceiling (incomplete history)")

// Activity is one row from /activity. We only consume the fields the win-rate
// algorithm needs; unknown fields are ignored.
type Activity struct {
	Type        string  `json:"type"`        // TRADE, REDEEM, MERGE, SPLIT, REWARD, ...
	Side        string  `json:"side"`        // BUY or SELL (TRADE only)
	Size        float64 `json:"size"`        // share quantity of the event
	USDCSize    float64 `json:"usdcSize"`    // cash size of the event
	ConditionID string  `json:"conditionId"` // market identifier
	Timestamp   int64   `json:"timestamp"`   // unix seconds of the event
}

// Client reads per-wallet data over HTTP via the shared data-api client.
type Client struct {
	api *pmapi.Client
}

// New returns a Client. An empty baseURL uses the shared default host; a nil hc
// uses a 15s timeout.
func New(baseURL string, hc *http.Client) *Client {
	return &Client{api: pmapi.New(baseURL, userAgent, hc)}
}

// Activity returns the wallet's activity rows, paginating in pages of
// activityPageSize until the history is exhausted (a short final page). maxRows is
// a SAFETY CEILING, not a target: it bounds a pathological wallet, and hitting it
// is recorded (mActivityCapped) so the resulting partial-history skew is visible.
// We never slice mid-page, since stats.Compute groups by conditionId and a cut in
// the middle of a market's events corrupts its cost basis. A non-positive maxRows
// returns no rows.
func (c *Client) Activity(ctx context.Context, wallet string, maxRows int) ([]Activity, error) {
	if maxRows <= 0 {
		return nil, nil
	}
	var all []Activity
	for offset := 0; ; offset += activityPageSize {
		q := url.Values{}
		q.Set("user", wallet)
		q.Set("limit", strconv.Itoa(activityPageSize))
		q.Set("offset", strconv.Itoa(offset))

		var page []Activity
		if err := c.api.GetJSON(ctx, "/activity", q, &page); err != nil {
			return nil, fmt.Errorf("data-api /activity: %w", err)
		}
		all = append(all, page...)
		if len(page) < activityPageSize {
			return all, nil // exhausted: full history fetched
		}
		if len(all) >= maxRows {
			mActivityCapped.Inc() // more history exists than the ceiling allows
			return all, nil
		}
	}
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

	var rows []valueRow
	if err := c.api.GetJSON(ctx, "/value", q, &rows); err != nil {
		return 0, fmt.Errorf("data-api /value: %w", err)
	}
	// Return the row that actually matches the queried wallet, not blindly rows[0]
	// — a mismatched-but-present row would otherwise bypass the no-portfolio
	// fallback and feed a wrong figure to the MinPortfolioUSD gate.
	for _, r := range rows {
		if strings.EqualFold(r.User, wallet) {
			return r.Value, nil
		}
	}
	return 0, nil
}
