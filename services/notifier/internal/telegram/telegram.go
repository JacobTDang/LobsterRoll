// Package telegram is a minimal Telegram Bot API client (sendMessage only).
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultBaseURL is the Telegram Bot API host.
const DefaultBaseURL = "https://api.telegram.org"

// Client sends messages as a bot.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New returns a Client. If baseURL is empty, DefaultBaseURL is used; if hc is
// nil, a 10s-timeout client is used.
func New(baseURL, token string, hc *http.Client) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{baseURL: baseURL, token: token, http: hc}
}

type sendMessageReq struct {
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

type apiResp struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
}

// Send posts text to chatID via sendMessage.
func (c *Client) Send(ctx context.Context, chatID, text string) error {
	body, err := json.Marshal(sendMessageReq{ChatID: chatID, Text: text})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	url := fmt.Sprintf("%s/bot%s/sendMessage", c.baseURL, c.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram status %d: %s", resp.StatusCode, raw)
	}
	var r apiResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if !r.OK {
		return fmt.Errorf("telegram error: %s", r.Description)
	}
	return nil
}
