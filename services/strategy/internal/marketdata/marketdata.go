// Package marketdata fetches live price and liquidity for a tokenId from the
// public Polymarket gamma API (same endpoint enrichment uses, different fields).
package marketdata

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

// DefaultBaseURL is the public gamma host.
const DefaultBaseURL = "https://gamma-api.polymarket.com"

const userAgent = "lobsterroll-strategy/1.0"

// Data is the live market context for a token.
type Data struct {
	CurrentPrice float64
	LiquidityUSD float64
	ConditionID  string
	Active       bool // active and not closed
}

type gammaMarket struct {
	ConditionID   string  `json:"conditionId"`
	LiquidityNum  float64 `json:"liquidityNum"`
	OutcomePrices string  `json:"outcomePrices"` // JSON-encoded array string
	ClobTokenIDs  string  `json:"clobTokenIds"`  // JSON-encoded array string
	Active        bool    `json:"active"`
	Closed        bool    `json:"closed"`
}

// Parse extracts market data for tokenID from a gamma markets response.
// ok=false (no error) means the token was not found.
func Parse(data []byte, tokenID string) (Data, bool, error) {
	var markets []gammaMarket
	if err := json.Unmarshal(data, &markets); err != nil {
		return Data{}, false, fmt.Errorf("decode gamma markets: %w", err)
	}
	for _, m := range markets {
		var tokens []string
		if err := json.Unmarshal([]byte(m.ClobTokenIDs), &tokens); err != nil {
			continue // skip a malformed sibling; don't fail a resolvable token
		}
		idx := indexOf(tokens, tokenID)
		if idx < 0 {
			continue
		}
		var prices []string
		if err := json.Unmarshal([]byte(m.OutcomePrices), &prices); err != nil {
			continue
		}
		if idx >= len(prices) {
			continue // outcomePrices shorter than clobTokenIds — treat as not found
		}
		price, err := strconv.ParseFloat(prices[idx], 64)
		if err != nil {
			return Data{}, false, fmt.Errorf("parse price %q: %w", prices[idx], err)
		}
		return Data{
			CurrentPrice: price,
			LiquidityUSD: m.LiquidityNum,
			ConditionID:  m.ConditionID,
			Active:       m.Active && !m.Closed,
		}, true, nil
	}
	return Data{}, false, nil
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// Client fetches market data over HTTP.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a Client. If hc is nil a 15s-timeout client is used.
func New(baseURL string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: baseURL, http: hc}
}

// Fetch resolves live market data for tokenID. ok=false means not found.
func (c *Client) Fetch(ctx context.Context, tokenID string) (Data, bool, error) {
	q := url.Values{}
	q.Set("clob_token_ids", tokenID)
	endpoint := c.baseURL + "/markets?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Data{}, false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return Data{}, false, fmt.Errorf("fetch market data: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Data{}, false, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Data{}, false, fmt.Errorf("gamma markets: status %d", resp.StatusCode)
	}
	return Parse(body, tokenID)
}
