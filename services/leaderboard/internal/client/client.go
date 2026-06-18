// Package client fetches the Polymarket leaderboard (top proxy wallets by pnl
// or volume over a time window) from the public lb-api host.
//
// Endpoint (verified live):
//
//	GET https://lb-api.polymarket.com/{profit|volume}?window={1d|7d|30d|all}&limit=N
//
// The response is a JSON array sorted by `amount` descending. Note that the
// host treats limit=0 as "no limit", so callers must request a positive topN.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/chain"
)

// DefaultBaseURL is the public Polymarket leaderboard host.
const DefaultBaseURL = "https://lb-api.polymarket.com"

// Metric selects which leaderboard to read.
type Metric string

const (
	MetricPNL    Metric = "pnl"    // served at /profit
	MetricVolume Metric = "volume" // served at /volume
)

// path returns the URL path for the metric, or an error if unknown.
func (m Metric) path() (string, error) {
	switch m {
	case MetricPNL:
		return "/profit", nil
	case MetricVolume:
		return "/volume", nil
	default:
		return "", fmt.Errorf("unknown metric %q", m)
	}
}

// Window is the leaderboard time window. The lb-api host accepts exactly these.
type Window string

var validWindows = map[Window]struct{}{
	"1d": {}, "7d": {}, "30d": {}, "all": {},
}

// ValidWindow reports whether w is an accepted lb-api window value.
func ValidWindow(w Window) bool {
	_, ok := validWindows[w]
	return ok
}

// Entry is one leaderboard row. We only consume the wallet and its metric value.
type Entry struct {
	Wallet string  `json:"proxyWallet"`
	Amount float64 `json:"amount"`
}

// ParseLeaderboard decodes a leaderboard JSON array, preserving the API's
// descending-by-amount order. It does not normalize addresses (see Fetch).
func ParseLeaderboard(data []byte) ([]Entry, error) {
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("decode leaderboard: %w", err)
	}
	return entries, nil
}

// Client reads the leaderboard over HTTP.
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

// Fetch returns up to topN normalized (lowercase, de-duplicated) wallet
// addresses for the given metric and window, in leaderboard order.
func (c *Client) Fetch(ctx context.Context, metric Metric, window Window, topN int) ([]string, error) {
	if topN <= 0 {
		return nil, fmt.Errorf("topN must be > 0, got %d (lb-api treats limit=0 as unlimited)", topN)
	}
	path, err := metric.path()
	if err != nil {
		return nil, err
	}
	if !ValidWindow(window) {
		return nil, fmt.Errorf("invalid window %q (want one of 1d, 7d, 30d, all)", window)
	}

	q := url.Values{}
	q.Set("window", string(window))
	q.Set("limit", strconv.Itoa(topN))
	endpoint := c.baseURL + path + "?" + q.Encode()

	body, err := c.fetchBody(ctx, endpoint, path)
	if err != nil {
		return nil, err
	}

	entries, err := ParseLeaderboard(body)
	if err != nil {
		return nil, err
	}

	wallets := make([]string, 0, len(entries))
	for _, e := range entries {
		wallets = append(wallets, e.Wallet)
	}
	wallets = chain.NormalizeAddresses(wallets)
	// An empty leaderboard is treated as a failed fetch: a transient
	// garbage-but-200 response must never wipe the downstream watchset.
	if len(wallets) == 0 {
		return nil, fmt.Errorf("leaderboard %s: returned no wallets", path)
	}
	if len(wallets) > topN {
		wallets = wallets[:topN]
	}
	return wallets, nil
}

// FetchEntries returns up to topN leaderboard entries (normalized wallet +
// metric amount) for the given metric and window, in leaderboard order. Unlike
// Fetch it preserves the per-wallet amount so callers can build a candidate
// pool keyed by e.g. 30d profit. Wallets are lowercased and de-duplicated
// (first-seen wins). An empty result is treated as a failed fetch.
func (c *Client) FetchEntries(ctx context.Context, metric Metric, window Window, topN int) ([]Entry, error) {
	if topN <= 0 {
		return nil, fmt.Errorf("topN must be > 0, got %d (lb-api treats limit=0 as unlimited)", topN)
	}
	path, err := metric.path()
	if err != nil {
		return nil, err
	}
	if !ValidWindow(window) {
		return nil, fmt.Errorf("invalid window %q (want one of 1d, 7d, 30d, all)", window)
	}

	q := url.Values{}
	q.Set("window", string(window))
	q.Set("limit", strconv.Itoa(topN))
	endpoint := c.baseURL + path + "?" + q.Encode()

	body, err := c.fetchBody(ctx, endpoint, path)
	if err != nil {
		return nil, err
	}
	entries, err := ParseLeaderboard(body)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(entries))
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		addr, ok := chain.NormalizeAddress(e.Wallet)
		if !ok {
			continue
		}
		if _, dup := seen[addr]; dup {
			continue
		}
		seen[addr] = struct{}{}
		out = append(out, Entry{Wallet: addr, Amount: e.Amount})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("leaderboard %s: returned no wallets", path)
	}
	if len(out) > topN {
		out = out[:topN]
	}
	return out, nil
}

// retry tuning. Kept short so tests run fast.
const (
	maxAttempts   = 3
	errBodyMaxLen = 256
)

var backoffSchedule = []time.Duration{100 * time.Millisecond, 200 * time.Millisecond}

// fetchBody performs the HTTP GET with a bounded retry/backoff loop. It retries
// on network errors and on transient HTTP statuses (429 or >=500), up to
// maxAttempts total. Other non-200 statuses (e.g. 4xx) fail immediately.
func (c *Client) fetchBody(ctx context.Context, endpoint, path string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// backoff before retrying; respect ctx cancellation.
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
	return nil, fmt.Errorf("leaderboard %s: giving up after %d attempts: %w", path, maxAttempts, lastErr)
}

// doOnce issues a single request. It returns transient=true when the failure is
// worth retrying (network error, or HTTP 429 / >=500).
func (c *Client) doOnce(ctx context.Context, endpoint, path string) (body []byte, transient bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, fmt.Errorf("build request: %w", err)
	}
	// Default Go User-Agent gets blocked by WAFs; identify ourselves.
	req.Header.Set("User-Agent", "lobsterroll-leaderboard/1.0")

	resp, err := c.http.Do(req)
	if err != nil {
		// Network-level failures are transient and worth retrying.
		return nil, true, fmt.Errorf("fetch leaderboard: %w", err)
	}
	defer resp.Body.Close()

	body, err = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, true, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		isTransient := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return nil, isTransient, fmt.Errorf("leaderboard %s: status %d: %s", path, resp.StatusCode, truncate(body, errBodyMaxLen))
	}
	return body, false, nil
}

// truncate bounds a response body for safe inclusion in error messages, so a
// 1MB error page never floods the logs.
func truncate(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + fmt.Sprintf("... (%d bytes truncated)", len(b)-max)
}
