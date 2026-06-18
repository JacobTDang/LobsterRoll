package bus

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestPublishDecision_Routing(t *testing.T) {
	url := runServer(t)
	raw, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer raw.Close()
	approved := make(chan *nats.Msg, 1)
	rejected := make(chan *nats.Msg, 1)
	_, _ = raw.ChanSubscribe(SubjectOrderApproved, approved)
	_, _ = raw.ChanSubscribe(SubjectOrderRejected, rejected)

	pub, err := Connect(url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer pub.Close()

	if err := pub.PublishDecision(OrderDecision{ProposalID: "p1", Approved: true, By: "telegram:me"}); err != nil {
		t.Fatalf("PublishDecision: %v", err)
	}
	if err := pub.PublishDecision(OrderDecision{ProposalID: "p2", Approved: false}); err != nil {
		t.Fatalf("PublishDecision: %v", err)
	}

	select {
	case m := <-approved:
		var d OrderDecision
		_ = json.Unmarshal(m.Data, &d)
		if d.ProposalID != "p1" || !d.Approved {
			t.Fatalf("approved msg = %+v", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no approved message")
	}
	select {
	case m := <-rejected:
		var d OrderDecision
		_ = json.Unmarshal(m.Data, &d)
		if d.ProposalID != "p2" || d.Approved {
			t.Fatalf("rejected msg = %+v", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no rejected message")
	}
}

func TestPublishControl(t *testing.T) {
	url := runServer(t)
	raw, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer raw.Close()
	msgs := make(chan *nats.Msg, 1)
	_, _ = raw.ChanSubscribe(SubjectControlHalt, msgs)

	pub, err := Connect(url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer pub.Close()
	if err := pub.PublishControl(ControlMsg{Halted: true, By: "telegram:me"}); err != nil {
		t.Fatalf("PublishControl: %v", err)
	}
	select {
	case m := <-msgs:
		var c ControlMsg
		_ = json.Unmarshal(m.Data, &c)
		if !c.Halted || c.By != "telegram:me" {
			t.Fatalf("control = %+v", c)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no control message")
	}
}

func TestOnOrderProposed(t *testing.T) {
	url := runServer(t)
	sub, err := NewSubscriber(url, nil)
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	defer sub.Close()
	got := make(chan OrderProposal, 1)
	if _, err := sub.OnOrderProposed("notifier", func(p OrderProposal) { got <- p }); err != nil {
		t.Fatalf("OnOrderProposed: %v", err)
	}

	pub, err := Connect(url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer pub.Close()
	if err := pub.PublishProposal(OrderProposal{ID: "p1", TokenID: "tok", Side: "buy", SizeUSD: 25}); err != nil {
		t.Fatalf("PublishProposal: %v", err)
	}
	select {
	case p := <-got:
		if p.ID != "p1" || p.SizeUSD != 25 {
			t.Fatalf("got %+v", p)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no proposal received")
	}
}
