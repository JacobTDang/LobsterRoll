// Package capture tracks which outcome tokens are actively traded and snapshots
// their midprice on each poll, so a closing line is on record by the time a
// market resolves.
package capture

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Pricer fetches a token's current midprice. *client.Client satisfies it.
type Pricer interface {
	Midpoint(ctx context.Context, tokenID string) (float64, error)
}

// Snapshotter persists a midprice snapshot. *store.Store satisfies it.
type Snapshotter interface {
	Put(ctx context.Context, tokenID string, ts int64, mid float64) error
}

// Tracker holds the set of tokens to snapshot, keyed by the last time each was
// traded. Tokens untraded for longer than ttl are dropped (their market has
// resolved — the close is already captured), bounding the polled set.
type Tracker struct {
	mu     sync.Mutex
	seen   map[string]int64
	pricer Pricer
	store  Snapshotter
	ttl    time.Duration
	log    *slog.Logger
}

// New constructs a Tracker. ttl bounds how long an untraded token keeps polling.
func New(p Pricer, s Snapshotter, ttl time.Duration, log *slog.Logger) *Tracker {
	return &Tracker{seen: make(map[string]int64), pricer: p, store: s, ttl: ttl, log: log}
}

// Track records that tokenID was traded at now (unix seconds), so it is polled.
func (t *Tracker) Track(tokenID string, now int64) {
	if tokenID == "" {
		return
	}
	t.mu.Lock()
	t.seen[tokenID] = now
	t.mu.Unlock()
}

// Active returns the number of tokens currently tracked.
func (t *Tracker) Active() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.seen)
}

// Poll snapshots the midprice of every active token at ts=now and prunes tokens
// untraded beyond ttl. The token list is snapshotted under lock; the network
// fetches run unlocked. A per-token fetch/store error is logged and skipped.
func (t *Tracker) Poll(ctx context.Context, now int64) {
	ttlSecs := int64(t.ttl.Seconds())
	t.mu.Lock()
	active := make([]string, 0, len(t.seen))
	for tok, last := range t.seen {
		if now-last > ttlSecs {
			delete(t.seen, tok)
			continue
		}
		active = append(active, tok)
	}
	t.mu.Unlock()

	for _, tok := range active {
		mid, err := t.pricer.Midpoint(ctx, tok)
		if err != nil {
			t.log.Warn("midpoint fetch failed; skipping snapshot", "token", tok, "err", err)
			continue
		}
		if err := t.store.Put(ctx, tok, now, mid); err != nil {
			t.log.Warn("snapshot store failed", "token", tok, "err", err)
		}
	}
}
