package bus

import (
	"testing"
	"time"
)

func TestSubscriber_OnTradeDetected(t *testing.T) {
	url := runServer(t) // helper from publish_test.go

	sub, err := NewSubscriber(url, nil)
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	defer sub.Close()

	got := make(chan TradeDetected, 1)
	if _, err := sub.OnTradeDetected("notifier", func(td TradeDetected) { got <- td }); err != nil {
		t.Fatalf("OnTradeDetected: %v", err)
	}

	pub, err := Connect(url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer pub.Close()

	want := TradeDetected{Wallet: "0xabc", Side: "buy", Price: "0.5", Size: "10", TxHash: "0xtx"}
	if err := pub.PublishTrade(want); err != nil {
		t.Fatalf("PublishTrade: %v", err)
	}

	select {
	case td := <-got:
		if td.Wallet != want.Wallet || td.Side != want.Side || td.Size != want.Size {
			t.Fatalf("got %+v, want %+v", td, want)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for delivered trade")
	}
}
