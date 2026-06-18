// Command mocktelegram is a tiny stand-in for the Telegram Bot API used by
// `make verify-alerts`. It accepts sendMessage (recording the text to -out),
// answers getUpdates with an empty list, and returns ok for everything else —
// so the real notifier binary can be exercised end-to-end with no real bot.
package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	addr := flag.String("addr", ":8099", "listen address")
	out := flag.String("out", ".local/alerts.log", "file to append received sendMessage texts")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			var msg struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(body, &msg)
			if f, err := os.OpenFile(*out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				_, _ = f.WriteString(msg.Text + "\n---\n")
				_ = f.Close()
			}
			log.Printf("sendMessage:\n%s", msg.Text)
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			time.Sleep(time.Second) // avoid a busy poll loop
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
		}
	})
	log.Printf("mocktelegram listening on %s (out=%s)", *addr, *out)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
