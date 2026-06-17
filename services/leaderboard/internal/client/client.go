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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch leaderboard: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("leaderboard %s: status %d: %s", path, resp.StatusCode, body)
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
	if len(wallets) > topN {
		wallets = wallets[:topN]
	}
	return wallets, nil
}
