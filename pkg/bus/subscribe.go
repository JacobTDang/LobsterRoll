package bus

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
)

// Subscriber consumes event-pipeline messages from NATS.
type Subscriber struct {
	nc  *nats.Conn
	log *slog.Logger
}

// NewSubscriber dials NATS at url. Like Connect, it fails fast on startup and
// reconnects indefinitely once connected. If log is nil, slog.Default() is used.
func NewSubscriber(url string, log *slog.Logger) (*Subscriber, error) {
	if log == nil {
		log = slog.Default()
	}
	nc, err := nats.Connect(url,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.Timeout(5*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("nats connect %q: %w", url, err)
	}
	return &Subscriber{nc: nc, log: log}, nil
}

// OnTradeDetected invokes handler for each TradeDetected on SubjectTradeDetected.
// Using a queue group lets multiple instances share the load. Messages that fail
// to decode are dropped (both producer and consumer are first-party).
func (s *Subscriber) OnTradeDetected(queue string, handler func(TradeDetected)) (*nats.Subscription, error) {
	return s.nc.QueueSubscribe(SubjectTradeDetected, queue, func(msg *nats.Msg) {
		var td TradeDetected
		if err := json.Unmarshal(msg.Data, &td); err != nil {
			s.log.Warn("dropping undecodable trades.detected message", "err", err)
			return
		}
		handler(td)
	})
}

// OnOrderProposed invokes handler for each OrderProposal on SubjectOrderProposed.
func (s *Subscriber) OnOrderProposed(queue string, handler func(OrderProposal)) (*nats.Subscription, error) {
	return s.nc.QueueSubscribe(SubjectOrderProposed, queue, func(msg *nats.Msg) {
		var p OrderProposal
		if err := json.Unmarshal(msg.Data, &p); err != nil {
			s.log.Warn("dropping undecodable orders.proposed message", "err", err)
			return
		}
		handler(p)
	})
}

// Close drains and closes the connection.
func (s *Subscriber) Close() {
	if s.nc != nil {
		_ = s.nc.Drain()
	}
}
