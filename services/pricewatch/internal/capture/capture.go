// Package capture tracks which outcome tokens are actively traded and snapshots
// their midprice on each poll, so a closing line is on record by the time a
// market resolves.
package capture

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/metrics"
)

var mSnapshots = metrics.NewCounter("lobsterroll_pricewatch_snapshots_total", "midprice snapshots stored")

// Pricer fetches a token's current midprice. *client.Client satisfies it.
type Pricer interface {
	Midpoint(ctx context.Context, tokenID string) (float64, error)
}

// Snapshotter persists a midprice snapshot. *store.Store satisfies it.
type Snapshotter interface {
	Put(ctx context.Context, tokenID string, ts int64, mid float64) error
}

// Tracker holds the set of tokens to snapshot, keyed by the last time each was
// traded. Tokens untraded for longer than ttl are dropped to bound the polled
// set. Eviction is NOT permanent and does NOT assume resolution: any later trade
// re-adds the token (Track), and markets typically see late trading near close
// — so the closing line is still captured. A fully-silent market's price isn't
// moving, so its last snapshot already serves as the close. (A future refinement
// could evict on the market's actual endDate via enrichment instead of ttl.)
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
			continue
		}
		mSnapshots.Inc()
	}
}
