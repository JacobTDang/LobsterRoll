// Package telegram is a minimal Telegram Bot API client: sending messages
// (plain and with inline keyboards), long-poll getUpdates, and answering/editing
// callbacks. All methods share a 429-aware request path.
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
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

const maxSendAttempts = 2

// InlineButton is one inline keyboard button.
type InlineButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type replyMarkup struct {
	InlineKeyboard [][]InlineButton `json:"inline_keyboard"`
}

// Update is a single getUpdates result (only the fields we use).
type Update struct {
	UpdateID      int            `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

// Message is a Telegram message.
type Message struct {
	MessageID int    `json:"message_id"`
	Text      string `json:"text"`
	From      User   `json:"from"`
	Chat      Chat   `json:"chat"`
}

// CallbackQuery is an inline-button tap.
type CallbackQuery struct {
	ID      string   `json:"id"`
	Data    string   `json:"data"`
	From    User     `json:"from"`
	Message *Message `json:"message"`
}

// User is the sender of a message/callback.
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// Chat identifies a chat.
type Chat struct {
	ID int64 `json:"id"`
}

// SendKeyboard sends text with an inline keyboard and returns the message id.
func (c *Client) SendKeyboard(ctx context.Context, chatID, text string, keyboard [][]InlineButton) (int, error) {
	var res struct {
		MessageID int `json:"message_id"`
	}
	payload := map[string]any{"chat_id": chatID, "text": text, "reply_markup": replyMarkup{InlineKeyboard: keyboard}}
	if err := c.call(ctx, "sendMessage", payload, &res); err != nil {
		return 0, err
	}
	return res.MessageID, nil
}

// AnswerCallback acknowledges a button tap (clears the client-side spinner).
func (c *Client) AnswerCallback(ctx context.Context, callbackID, text string) error {
	return c.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": callbackID, "text": text}, nil)
}

// EditMessageText replaces a message's text and removes its inline keyboard.
func (c *Client) EditMessageText(ctx context.Context, chatID string, messageID int, text string) error {
	payload := map[string]any{
		"chat_id":      chatID,
		"message_id":   messageID,
		"text":         text,
		"reply_markup": replyMarkup{InlineKeyboard: [][]InlineButton{}},
	}
	return c.call(ctx, "editMessageText", payload, nil)
}

// pollBuffer is the headroom added to a long-poll's own request deadline so the
// request is never cut short by less than the server's hold time.
const pollBuffer = 15 * time.Second

// GetUpdates long-polls for updates after offset, waiting up to timeoutSec. The
// request is given its own deadline of timeoutSec+pollBuffer so it can't be cut
// short regardless of the http client's configured timeout.
func (c *Client) GetUpdates(ctx context.Context, offset, timeoutSec int) ([]Update, error) {
	rctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second+pollBuffer)
	defer cancel()
	var out []Update
	err := c.call(rctx, "getUpdates", map[string]any{"offset": offset, "timeout": timeoutSec}, &out)
	return out, err
}

// Send posts text to chatID via sendMessage.
func (c *Client) Send(ctx context.Context, chatID, text string) error {
	return c.call(ctx, "sendMessage", sendMessageReq{ChatID: chatID, Text: text}, nil)
}

// call POSTs a Bot API method and unmarshals the `result` field into out (if
// non-nil). On HTTP 429 it honors retry_after and retries (ctx-aware). It
// returns an error on transport failure, non-200, or ok:false.
func (c *Client) call(ctx context.Context, method string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", method, err)
	}
	url := fmt.Sprintf("%s/bot%s/%s", c.baseURL, c.token, method)

	for attempt := 0; attempt < maxSendAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("build %s request: %w", method, err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("%s: %w", method, err)
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxSendAttempts-1 {
			var r apiResp
			_ = json.Unmarshal(raw, &r)
			wait := time.Duration(r.Parameters.RetryAfter) * time.Second
			if wait > 60*time.Second {
				wait = 60 * time.Second
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%s: status %d: %s", method, resp.StatusCode, truncate(raw, 256))
		}

		var r apiResp
		if err := json.Unmarshal(raw, &r); err != nil {
			return fmt.Errorf("%s decode: %w", method, err)
		}
		if !r.OK {
			return fmt.Errorf("%s: telegram error: %s", method, r.Description)
		}
		if out != nil && len(r.Result) > 0 {
			if err := json.Unmarshal(r.Result, out); err != nil {
				return fmt.Errorf("%s decode result: %w", method, err)
			}
		}
		return nil
	}
	return fmt.Errorf("%s: gave up after %d attempts (rate limited)", method, maxSendAttempts)
}

func truncate(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "…"
}
