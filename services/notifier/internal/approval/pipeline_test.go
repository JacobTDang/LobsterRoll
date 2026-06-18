package approval

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
	"github.com/JacobTDang/LobsterRoll/services/notifier/internal/telegram"
)

// TestPipeline_ProposalToApproved wires the real bus: a published
// orders.proposed becomes a button message, and a simulated ✅ tap publishes
// orders.approved.
func TestPipeline_ProposalToApproved(t *testing.T) {
	opts := natsserver.DefaultTestOptions
	opts.Port = -1
	ns := natsserver.RunServer(&opts)
	defer ns.Shutdown()
	url := ns.ClientURL()

	raw, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer raw.Close()
	approved := make(chan *nats.Msg, 1)
	if _, err := raw.ChanSubscribe(bus.SubjectOrderApproved, approved); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	pub, err := bus.Connect(url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer pub.Close()
	sub, err := bus.NewSubscriber(url, nil)
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	defer sub.Close()

	tg := &fakeTG{}
	mgr := New(tg, pub, "55", quiet())
	ctx := context.Background()
	if _, err := sub.OnOrderProposed("notifier", func(p bus.OrderProposal) { mgr.OnProposal(ctx, p) }); err != nil {
		t.Fatalf("OnOrderProposed: %v", err)
	}

	if err := pub.PublishProposal(bus.OrderProposal{ID: "prop-XYZ", TokenID: "tok", Side: "buy", LimitPrice: "0.98", SizeUSD: 25}); err != nil {
		t.Fatalf("PublishProposal: %v", err)
	}

	// Wait for the button message to be sent.
	deadline := time.Now().Add(5 * time.Second)
	for {
		tg.mu.Lock()
		n := tg.keyboards
		tg.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("proposal button message not sent")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Simulate the operator tapping ✅ (key "1" from the first proposal).
	mgr.HandleCallback(ctx, telegram.CallbackQuery{ID: "cb", Data: "a:1", From: telegram.User{Username: "jacob"}})

	select {
	case m := <-approved:
		var d bus.OrderDecision
		_ = json.Unmarshal(m.Data, &d)
		if d.ProposalID != "prop-XYZ" || !d.Approved || d.By != "telegram:jacob" {
			t.Fatalf("decision = %+v", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("orders.approved not published after approve tap")
	}
}
