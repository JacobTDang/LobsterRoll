// Command tgchatid prints the chat id(s) that have messaged your bot, so you can
// set TELEGRAM_CHAT_ID. Message your bot once first, then run:
//
//	TELEGRAM_BOT_TOKEN=... go run ./tools/tgchatid
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required")
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Get("https://api.telegram.org/bot" + token + "/getUpdates")
	if err != nil {
		log.Fatalf("getUpdates: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var r struct {
		OK     bool `json:"ok"`
		Result []struct {
			Message struct {
				Chat struct {
					ID    int64  `json:"id"`
					Type  string `json:"type"`
					Title string `json:"title"`
				} `json:"chat"`
				From struct {
					Username string `json:"username"`
				} `json:"from"`
			} `json:"message"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &r); err != nil || !r.OK {
		log.Fatalf("unexpected response: %s", body)
	}
	if len(r.Result) == 0 {
		fmt.Println("No updates yet — open your bot in Telegram and send it any message, then re-run.")
		return
	}
	seen := map[int64]bool{}
	for _, u := range r.Result {
		c := u.Message.Chat
		if c.ID == 0 || seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		label := "@" + u.Message.From.Username
		if c.Title != "" {
			label = c.Title
		}
		fmt.Printf("TELEGRAM_CHAT_ID=%d   # %s (%s)\n", c.ID, label, c.Type)
	}
}
