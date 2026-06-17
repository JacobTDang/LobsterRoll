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
