package bus

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestPublisher_PublishProposal(t *testing.T) {
	url := runServer(t) // helper from publish_test.go

	sub, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("subscriber connect: %v", err)
	}
	defer sub.Close()
	msgs := make(chan *nats.Msg, 1)
	if _, err := sub.ChanSubscribe(SubjectOrderProposed, msgs); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	pub, err := Connect(url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer pub.Close()

	want := OrderProposal{ID: "prop-1", TokenID: "tok", Side: "buy", LimitPrice: "0.98", SizeUSD: 25, Reason: "mirror"}
	if err := pub.PublishProposal(want); err != nil {
		t.Fatalf("PublishProposal: %v", err)
	}

	select {
	case m := <-msgs:
		var got OrderProposal
		if err := json.Unmarshal(m.Data, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.ID != want.ID || got.SizeUSD != want.SizeUSD || got.LimitPrice != want.LimitPrice {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for proposal")
	}
}
