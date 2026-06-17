package bus

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// Subscriber consumes event-pipeline messages from NATS.
type Subscriber struct {
	nc *nats.Conn
}

// NewSubscriber dials NATS at url. Like Connect, it fails fast on startup and
// reconnects indefinitely once connected.
func NewSubscriber(url string) (*Subscriber, error) {
	nc, err := nats.Connect(url,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.Timeout(5*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("nats connect %q: %w", url, err)
	}
	return &Subscriber{nc: nc}, nil
}

// OnTradeDetected invokes handler for each TradeDetected on SubjectTradeDetected.
// Using a queue group lets multiple instances share the load. Messages that fail
// to decode are dropped (both producer and consumer are first-party).
func (s *Subscriber) OnTradeDetected(queue string, handler func(TradeDetected)) (*nats.Subscription, error) {
	return s.nc.QueueSubscribe(SubjectTradeDetected, queue, func(msg *nats.Msg) {
		var td TradeDetected
		if err := json.Unmarshal(msg.Data, &td); err != nil {
			return
		}
		handler(td)
	})
}

// Close drains and closes the connection.
func (s *Subscriber) Close() {
	if s.nc != nil {
		_ = s.nc.Drain()
	}
}
