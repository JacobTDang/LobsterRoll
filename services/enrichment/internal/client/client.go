// Package client resolves a Polymarket tokenId into market question/outcome via
// the public gamma API.
//
// Endpoint (verified live):
//
//	GET https://gamma-api.polymarket.com/markets?clob_token_ids=<tokenId>
//
// Returns a JSON array (empty => unknown token). Note that `outcomes` and
// `clobTokenIds` are themselves JSON-encoded strings and must be double-parsed.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// DefaultBaseURL is the public Polymarket gamma host.
const DefaultBaseURL = "https://gamma-api.polymarket.com"

const userAgent = "lobsterroll-enrichment/1.0"

// Enrichment is a resolved tokenId.
type Enrichment struct {
	MarketQuestion string
	Outcome        string
	MarketSlug     string
	ConditionID    string
}

type gammaMarket struct {
	Question     string `json:"question"`
	Slug         string `json:"slug"`
	ConditionID  string `json:"conditionId"`
	Outcomes     string `json:"outcomes"`     // JSON-encoded array string
	ClobTokenIDs string `json:"clobTokenIds"` // JSON-encoded array string
}

// Resolve parses a gamma markets response and returns the enrichment for
// tokenID. ok=false (no error) means the token was not found.
func Resolve(data []byte, tokenID string) (Enrichment, bool, error) {
	var markets []gammaMarket
	if err := json.Unmarshal(data, &markets); err != nil {
		return Enrichment{}, false, fmt.Errorf("decode gamma markets: %w", err)
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
		var outcomes []string
		if err := json.Unmarshal([]byte(m.Outcomes), &outcomes); err != nil {
			continue
		}
		outcome := ""
		if idx < len(outcomes) {
			outcome = outcomes[idx]
		}
		return Enrichment{
			MarketQuestion: m.Question,
			Outcome:        outcome,
			MarketSlug:     m.Slug,
			ConditionID:    m.ConditionID,
		}, true, nil
	}
	return Enrichment{}, false, nil
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// Client fetches enrichment over HTTP.
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

// Fetch resolves tokenID. ok=false (no error) means not found.
func (c *Client) Fetch(ctx context.Context, tokenID string) (Enrichment, bool, error) {
	q := url.Values{}
	q.Set("clob_token_ids", tokenID)
	endpoint := c.baseURL + "/markets?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Enrichment{}, false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return Enrichment{}, false, fmt.Errorf("fetch enrichment: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Enrichment{}, false, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Enrichment{}, false, fmt.Errorf("gamma markets: status %d", resp.StatusCode)
	}
	return Resolve(body, tokenID)
}
