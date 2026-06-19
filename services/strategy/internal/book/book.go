// Package book reads the Polymarket CLOB order book for a token and derives the
// midprice, half-spread, and near-side depth that the sizing engine needs.
package book

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/httpx"
)

// DefaultBaseURL is the public Polymarket CLOB host.
const DefaultBaseURL = "https://clob.polymarket.com"

const userAgent = "lobsterroll-strategy/1.0"

// Level is a price level: a price (0..1) and the size resting there.
type Level struct {
	Price float64
	Size  float64
}

// Book is a parsed order book with bids sorted high→low and asks low→high.
type Book struct {
	Bids []Level
	Asks []Level
}

// Mid returns the midprice; ok=false if either side is empty.
func (b Book) Mid() (float64, bool) {
	if len(b.Bids) == 0 || len(b.Asks) == 0 {
		return 0, false
	}
	return (b.Bids[0].Price + b.Asks[0].Price) / 2, true
}

// HalfSpread returns half the bid/ask spread; ok=false if either side is empty.
func (b Book) HalfSpread() (float64, bool) {
	if len(b.Bids) == 0 || len(b.Asks) == 0 {
		return 0, false
	}
	return (b.Asks[0].Price - b.Bids[0].Price) / 2, true
}

// BuyDepthUSD returns the USD fillable on the ask side within slippageBand of the
// best ask (price units) — the cap on how much we can buy without moving the
// price more than tolerated. Returns 0 if there are no asks.
func (b Book) BuyDepthUSD(slippageBand float64) float64 {
	if len(b.Asks) == 0 {
		return 0
	}
	limit := b.Asks[0].Price + slippageBand
	var usd float64
	for _, lvl := range b.Asks {
		if lvl.Price > limit {
			break
		}
		usd += lvl.Price * lvl.Size // notional at this level
	}
	return usd
}

// Client fetches order books over HTTP.
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

type jsonLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// Book fetches and parses the order book for tokenID.
func (c *Client) Book(ctx context.Context, tokenID string) (Book, error) {
	q := url.Values{}
	q.Set("token_id", tokenID)
	body, err := httpx.Get(ctx, c.http, c.baseURL+"/book?"+q.Encode(), userAgent, 1<<20)
	if err != nil {
		return Book{}, fmt.Errorf("book %s: %w", tokenID, err)
	}
	var raw struct {
		Bids []jsonLevel `json:"bids"`
		Asks []jsonLevel `json:"asks"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Book{}, fmt.Errorf("decode book %s: %w", tokenID, err)
	}
	bids, err := parseLevels(raw.Bids)
	if err != nil {
		return Book{}, fmt.Errorf("bids %s: %w", tokenID, err)
	}
	asks, err := parseLevels(raw.Asks)
	if err != nil {
		return Book{}, fmt.Errorf("asks %s: %w", tokenID, err)
	}
	// Best bid = highest price; best ask = lowest price. Don't trust input order.
	sort.Slice(bids, func(i, j int) bool { return bids[i].Price > bids[j].Price })
	sort.Slice(asks, func(i, j int) bool { return asks[i].Price < asks[j].Price })
	return Book{Bids: bids, Asks: asks}, nil
}

func parseLevels(in []jsonLevel) ([]Level, error) {
	out := make([]Level, 0, len(in))
	for _, l := range in {
		p, err := strconv.ParseFloat(l.Price, 64)
		if err != nil {
			return nil, fmt.Errorf("price %q: %w", l.Price, err)
		}
		s, err := strconv.ParseFloat(l.Size, 64)
		if err != nil {
			return nil, fmt.Errorf("size %q: %w", l.Size, err)
		}
		out = append(out, Level{Price: p, Size: s})
	}
	return out, nil
}
