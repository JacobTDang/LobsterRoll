package bus

import (
	"encoding/json"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
)

func runServer(t *testing.T) string {
	t.Helper()
	opts := natsserver.DefaultTestOptions
	opts.Port = -1 // random free port
	s := natsserver.RunServer(&opts)
	t.Cleanup(s.Shutdown)
	return s.ClientURL()
}

func TestPublisher_PublishTrade(t *testing.T) {
	url := runServer(t)

	// Raw subscriber to observe what the publisher emits.
	sub, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("subscriber connect: %v", err)
	}
	defer sub.Close()
	msgs := make(chan *nats.Msg, 1)
	if _, err := sub.ChanSubscribe(SubjectTradeDetected, msgs); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	pub, err := Connect(url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer pub.Close()

	want := TradeDetected{
		Wallet: "0xabc", TokenID: "123", Side: "buy",
		Price: "0.95", Size: "5.76", TxHash: "0xdead", LogIndex: 7, BlockNumber: 42,
		ObservedAt: time.Unix(1700000000, 0).UTC(),
	}
	if err := pub.PublishTrade(want); err != nil {
		t.Fatalf("PublishTrade: %v", err)
	}

	select {
	case m := <-msgs:
		var got TradeDetected
		if err := json.Unmarshal(m.Data, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Wallet != want.Wallet || got.Side != want.Side || got.Price != want.Price ||
			got.Size != want.Size || got.TxHash != want.TxHash || got.LogIndex != want.LogIndex {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for published trade")
	}
}

func TestConnect_BadURL(t *testing.T) {
	if _, err := Connect("nats://127.0.0.1:1"); err == nil {
		t.Error("expected error connecting to a dead address")
	}
}
