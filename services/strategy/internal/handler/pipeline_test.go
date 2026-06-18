package handler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
)

// TestPipeline_EndToEnd wires the real bus (embedded NATS): a published
// trades.detected with good market data yields an orders.proposed message.
func TestPipeline_EndToEnd(t *testing.T) {
	opts := natsserver.DefaultTestOptions
	opts.Port = -1
	ns := natsserver.RunServer(&opts)
	defer ns.Shutdown()
	url := ns.ClientURL()

	// Raw subscriber on orders.proposed.
	raw, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("raw connect: %v", err)
	}
	defer raw.Close()
	props := make(chan *nats.Msg, 1)
	if _, err := raw.ChanSubscribe(bus.SubjectOrderProposed, props); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	pub, err := bus.Connect(url)
	if err != nil {
		t.Fatalf("publisher connect: %v", err)
	}
	defer pub.Close()
	sub, err := bus.NewSubscriber(url, nil)
	if err != nil {
		t.Fatalf("subscriber connect: %v", err)
	}
	defer sub.Close()

	h := New(&fakeSrc{data: goodData(), ok: true}, pub, policy, nil, quiet())
	ctx := context.Background()
	if _, err := sub.OnTradeDetected("strategy", func(td bus.TradeDetected) { h.Handle(ctx, td) }); err != nil {
		t.Fatalf("OnTradeDetected: %v", err)
	}
	if err := sub.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	if err := pub.PublishTrade(trade); err != nil {
		t.Fatalf("PublishTrade: %v", err)
	}

	select {
	case m := <-props:
		var p bus.OrderProposal
		if err := json.Unmarshal(m.Data, &p); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if p.ID != "prop-0xabc-7-0xwhale" || p.SizeUSD != 25 {
			t.Fatalf("proposal = %+v", p)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for orders.proposed")
	}
}
