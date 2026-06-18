package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/test"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/telegram"
)

// TestPipeline_EndToEnd wires the real bus subscriber + handler + Telegram client
// (against a mock Telegram server) and verifies a published trade produces a
// correctly formatted alert. Exercises everything except the real bot token.
func TestPipeline_EndToEnd(t *testing.T) {
	// Embedded NATS.
	opts := natsserver.DefaultTestOptions
	opts.Port = -1
	ns := natsserver.RunServer(&opts)
	defer ns.Shutdown()
	url := ns.ClientURL()

	// Mock Telegram server captures the sent text.
	gotText := make(chan string, 1)
	tgsrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var msg struct {
			ChatID string `json:"chat_id"`
			Text   string `json:"text"`
		}
		_ = json.Unmarshal(b, &msg)
		gotText <- msg.Text
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer tgsrv.Close()

	tg := telegram.New(tgsrv.URL, "TOK", tgsrv.Client())
	enr := fakeEnricher{resp: &lobsterrollv1.EnrichTokenResponse{MarketQuestion: "Will it rain?", Outcome: "Yes"}}
	h := New(enr, tg, "555", quiet())

	sub, err := bus.NewSubscriber(url, nil)
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	defer sub.Close()
	ctx := context.Background()
	if _, err := sub.OnTradeDetected("notifier", func(td bus.TradeDetected) { h.Handle(ctx, td) }); err != nil {
		t.Fatalf("OnTradeDetected: %v", err)
	}
	if err := sub.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	pub, err := bus.Connect(url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer pub.Close()
	if err := pub.PublishTrade(trade); err != nil {
		t.Fatalf("PublishTrade: %v", err)
	}

	select {
	case text := <-gotText:
		if want := "Will it rain? → Yes"; !strings.Contains(text, want) {
			t.Fatalf("alert missing %q:\n%s", want, text)
		}
		if !strings.Contains(text, "🟢 ENTER (BUY)") {
			t.Fatalf("alert missing side marker:\n%s", text)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for alert to reach Telegram mock")
	}
}
