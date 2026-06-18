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
	nc     *nats.Conn
	log    *slog.Logger
	closed chan struct{} // closed when the connection has fully closed (post-drain)
}

// NewSubscriber dials NATS at url. Like Connect, it fails fast on startup and
// reconnects indefinitely once connected. If log is nil, slog.Default() is used.
func NewSubscriber(url string, log *slog.Logger) (*Subscriber, error) {
	if log == nil {
		log = slog.Default()
	}
	closed := make(chan struct{})
	nc, err := nats.Connect(url,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.Timeout(5*time.Second),
		nats.ClosedHandler(func(*nats.Conn) { close(closed) }),
	)
	if err != nil {
		return nil, fmt.Errorf("nats connect %q: %w", url, err)
	}
	return &Subscriber{nc: nc, log: log, closed: closed}, nil
}

// queueSubscribe is the shared body of the typed On* helpers: it JSON-decodes
// each message into T and invokes handler, dropping undecodable messages (both
// producer and consumer are first-party). A queue group lets multiple instances
// share the load. (A free function, not a method, since Go methods can't be
// generic.)
func queueSubscribe[T any](s *Subscriber, subject, queue, kind string, handler func(T)) (*nats.Subscription, error) {
	return s.nc.QueueSubscribe(subject, queue, func(msg *nats.Msg) {
		var v T
		if err := json.Unmarshal(msg.Data, &v); err != nil {
			s.log.Warn("dropping undecodable "+kind+" message", "err", err)
			return
		}
		handler(v)
	})
}

// OnTradeDetected invokes handler for each TradeDetected on SubjectTradeDetected.
func (s *Subscriber) OnTradeDetected(queue string, handler func(TradeDetected)) (*nats.Subscription, error) {
	return queueSubscribe(s, SubjectTradeDetected, queue, "trades.detected", handler)
}

// OnConsensus invokes handler for each ConsensusSignal on SubjectConsensusSignal.
func (s *Subscriber) OnConsensus(queue string, handler func(ConsensusSignal)) (*nats.Subscription, error) {
	return queueSubscribe(s, SubjectConsensusSignal, queue, "consensus.signal", handler)
}

// OnOrderProposed invokes handler for each OrderProposal on SubjectOrderProposed.
func (s *Subscriber) OnOrderProposed(queue string, handler func(OrderProposal)) (*nats.Subscription, error) {
	return queueSubscribe(s, SubjectOrderProposed, queue, "orders.proposed", handler)
}

// OnOrderApproved invokes handler for each approved OrderDecision.
func (s *Subscriber) OnOrderApproved(queue string, handler func(OrderDecision)) (*nats.Subscription, error) {
	return queueSubscribe(s, SubjectOrderApproved, queue, "orders.approved", handler)
}

// OnControl invokes handler for each control.halt message. It uses a plain
// (non-queue) subscription so every instance receives the kill switch.
func (s *Subscriber) OnControl(handler func(ControlMsg)) (*nats.Subscription, error) {
	return s.nc.Subscribe(SubjectControlHalt, func(msg *nats.Msg) {
		var c ControlMsg
		if err := json.Unmarshal(msg.Data, &c); err != nil {
			s.log.Warn("dropping undecodable control.halt message", "err", err)
			return
		}
		handler(c)
	})
}

// Flush blocks until the server has processed all prior protocol messages
// (e.g. subscriptions), so a publisher can be sure subscriptions are live.
func (s *Subscriber) Flush() error { return s.nc.Flush() }

// Close drains the connection (letting in-flight handlers finish) and waits,
// bounded, for it to fully close so the process doesn't exit mid-handler.
func (s *Subscriber) Close() {
	if s.nc == nil {
		return
	}
	if err := s.nc.Drain(); err != nil {
		s.nc.Close()
		return
	}
	select {
	case <-s.closed:
	case <-time.After(6 * time.Second):
		s.nc.Close() // drain overran; force close
	}
}
