package bus

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// Publisher publishes event-pipeline messages to NATS.
type Publisher struct {
	nc *nats.Conn
}

// Connect dials NATS at url and returns a Publisher. It fails fast if the
// server is unreachable rather than buffering silently.
func Connect(url string) (*Publisher, error) {
	// Fail fast if NATS is unreachable at startup (let the orchestrator restart
	// us), but reconnect indefinitely once a connection has been established so a
	// transient NATS blip mid-run self-heals.
	nc, err := nats.Connect(url,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.Timeout(5*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("nats connect %q: %w", url, err)
	}
	return &Publisher{nc: nc}, nil
}

// PublishTrade publishes a detected trade on SubjectTradeDetected.
func (p *Publisher) PublishTrade(t TradeDetected) error {
	return p.publishJSON(SubjectTradeDetected, t)
}

// PublishProposal publishes an order proposal on SubjectOrderProposed.
func (p *Publisher) PublishProposal(o OrderProposal) error {
	return p.publishJSON(SubjectOrderProposed, o)
}

// PublishDecision publishes an approve/reject decision, routed to
// SubjectOrderApproved or SubjectOrderRejected by d.Approved.
func (p *Publisher) PublishDecision(d OrderDecision) error {
	subject := SubjectOrderRejected
	if d.Approved {
		subject = SubjectOrderApproved
	}
	return p.publishJSON(subject, d)
}

// PublishControl publishes a halt/resume control message on SubjectControlHalt.
func (p *Publisher) PublishControl(c ControlMsg) error {
	return p.publishJSON(SubjectControlHalt, c)
}

// PublishResult publishes an execution result. A result with no Err (the order
// was accepted by the exchange — matched or resting) goes to SubjectOrderFilled;
// a result carrying an Err (rejected/failed/ambiguous) goes to SubjectOrderFailed.
// Filled distinguishes a true fill from a resting order within the success case.
func (p *Publisher) PublishResult(r OrderResult) error {
	subject := SubjectOrderFilled
	if r.Err != "" {
		subject = SubjectOrderFailed
	}
	return p.publishJSON(subject, r)
}

// PublishConsensus publishes a consensus signal on SubjectConsensusSignal.
func (p *Publisher) PublishConsensus(c ConsensusSignal) error {
	return p.publishJSON(SubjectConsensusSignal, c)
}

func (p *Publisher) publishJSON(subject string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", subject, err)
	}
	if err := p.nc.Publish(subject, data); err != nil {
		return fmt.Errorf("publish %s: %w", subject, err)
	}
	return nil
}

// Close flushes pending messages and closes the connection.
func (p *Publisher) Close() {
	if p.nc != nil {
		_ = p.nc.Flush()
		p.nc.Close()
	}
}
