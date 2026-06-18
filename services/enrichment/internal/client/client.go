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
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/httpx"
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
	EndDateUnix    int64 // market/game end time (unix secs); 0 if unknown
}

type gammaMarket struct {
	Question     string `json:"question"`
	Slug         string `json:"slug"`
	ConditionID  string `json:"conditionId"`
	Outcomes     string `json:"outcomes"`     // JSON-encoded array string
	ClobTokenIDs string `json:"clobTokenIds"` // JSON-encoded array string
	EndDate      string `json:"endDate"`      // ISO-8601, e.g. 2026-06-27T21:00:00Z
}

// parseEndDate converts a gamma ISO-8601 endDate to unix seconds; 0 if absent
// or unparseable (treated as "unknown" — never used to filter).
func parseEndDate(s string) int64 {
	if s == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return 0
	}
	return t.Unix()
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
		idx := slices.Index(tokens, tokenID)
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
			EndDateUnix:    parseEndDate(m.EndDate),
		}, true, nil
	}
	return Enrichment{}, false, nil
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

// Fetch resolves tokenID. ok=false (no error) means not found. The GET goes
// through httpx for a shared User-Agent + bounded retry/backoff on transient
// failures (429/5xx/network), matching the other Polymarket clients.
func (c *Client) Fetch(ctx context.Context, tokenID string) (Enrichment, bool, error) {
	q := url.Values{}
	q.Set("clob_token_ids", tokenID)
	endpoint := c.baseURL + "/markets?" + q.Encode()

	body, err := httpx.Get(ctx, c.http, endpoint, userAgent, 1<<20)
	if err != nil {
		return Enrichment{}, false, fmt.Errorf("gamma markets: %w", err)
	}
	return Resolve(body, tokenID)
}
