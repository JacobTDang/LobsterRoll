// Package clob is a minimal Polymarket CLOB client: L2-HMAC-authenticated order
// placement.
//
// L2 auth (per request): POLY_SIGNATURE = base64url(HMAC_SHA256(
//   base64url_decode(secret), timestamp + method + requestPath + body)).
//
// NOTE: the exact /order payload field names must be confirmed against the live
// CLOB before placing a real order (see the gated integration test).
package clob

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// DefaultBaseURL is the public Polymarket CLOB host.
const DefaultBaseURL = "https://clob.polymarket.com"

// Creds are the derived L2 API credentials.
type Creds struct {
	APIKey     string
	Secret     string // base64url-encoded
	Passphrase string
	Address    string // signer/operator address (POLY_ADDRESS)
}

// l2Sign computes the POLY_SIGNATURE for a request.
func l2Sign(secret, timestamp, method, path, body string) (string, error) {
	key, err := base64.URLEncoding.DecodeString(secret)
	if err != nil {
		return "", fmt.Errorf("decode secret: %w", err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(timestamp + method + path + body))
	return base64.URLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// SignedOrder is the order payload sent to the CLOB. Amounts/ids are decimal
// strings; side is "BUY"/"SELL".
type SignedOrder struct {
	Salt          string `json:"salt"`
	Maker         string `json:"maker"`
	Signer        string `json:"signer"`
	TokenID       string `json:"tokenId"`
	MakerAmount   string `json:"makerAmount"`
	TakerAmount   string `json:"takerAmount"`
	Side          string `json:"side"`
	SignatureType int    `json:"signatureType"`
	Timestamp     string `json:"timestamp"`
	Metadata      string `json:"metadata"`
	Builder       string `json:"builder"`
	Signature     string `json:"signature"`
}

type placeReq struct {
	Order     SignedOrder `json:"order"`
	Owner     string      `json:"owner"`
	OrderType string      `json:"orderType"`
}

// PlaceResult is the parsed response of a successful placement.
type PlaceResult struct {
	Success bool
	OrderID string
	Status  string // e.g. "matched", "live", "unmatched"
}

// placeResp mirrors the CLOB response. success is a pointer so an absent field
// (a 2xx body that omits it) is not treated as a rejection.
type placeResp struct {
	Success  *bool  `json:"success"`
	Error    string `json:"error"`
	ErrorMsg string `json:"errorMsg"`
	OrderID  string `json:"orderID"`
	Status   string `json:"status"`
}

// Client posts orders to the CLOB.
type Client struct {
	baseURL string
	creds   Creds
	http    *http.Client
	now     func() time.Time
}

// New returns a Client. If hc is nil a 15s-timeout client is used.
func New(baseURL string, creds Creds, hc *http.Client) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: baseURL, creds: creds, http: hc, now: time.Now}
}

// PlaceOrder posts a signed order with L2 auth headers and returns the result.
// A non-2xx or success:false response is an error (so the handler publishes
// orders.failed).
func (c *Client) PlaceOrder(ctx context.Context, o SignedOrder) (PlaceResult, error) {
	const path = "/order"
	body, err := json.Marshal(placeReq{Order: o, Owner: c.creds.APIKey, OrderType: "GTC"})
	if err != nil {
		return PlaceResult{}, fmt.Errorf("marshal order: %w", err)
	}
	ts := strconv.FormatInt(c.now().Unix(), 10)
	sig, err := l2Sign(c.creds.Secret, ts, http.MethodPost, path, string(body))
	if err != nil {
		return PlaceResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return PlaceResult{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("POLY_ADDRESS", c.creds.Address)
	req.Header.Set("POLY_API_KEY", c.creds.APIKey)
	req.Header.Set("POLY_PASSPHRASE", c.creds.Passphrase)
	req.Header.Set("POLY_TIMESTAMP", ts)
	req.Header.Set("POLY_SIGNATURE", sig)

	resp, err := c.http.Do(req)
	if err != nil {
		return PlaceResult{}, fmt.Errorf("place order: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return PlaceResult{}, fmt.Errorf("clob status %d: %s", resp.StatusCode, raw)
	}
	var r placeResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return PlaceResult{}, fmt.Errorf("decode response: %w", err)
	}
	// Treat a 2xx as success unless the body explicitly says otherwise.
	if (r.Success != nil && !*r.Success) || r.Error != "" || r.ErrorMsg != "" {
		return PlaceResult{}, fmt.Errorf("clob rejected order %s: status=%s err=%s%s", r.OrderID, r.Status, r.Error, r.ErrorMsg)
	}
	return PlaceResult{Success: true, OrderID: r.OrderID, Status: r.Status}, nil
}
