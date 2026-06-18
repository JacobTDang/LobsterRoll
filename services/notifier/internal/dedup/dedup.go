// Package dedup is a small time-bounded set: a key is "seen" for a TTL after it
// is added, so the notifier suppresses duplicate alerts (the watcher is
// at-least-once and can re-emit a trade). Memory is bounded by the TTL via a
// periodic sweep of expired keys.
package dedup

import (
	"sync"
	"time"
)

// TTLSet remembers keys for a fixed time-to-live.
type TTLSet struct {
	mu  sync.Mutex
	ttl time.Duration
	now func() time.Time
	exp map[string]time.Time
	ops int // adds since the last sweep
}

// sweepEvery bounds how often the expired-key sweep runs.
const sweepEvery = 512

// New returns a TTLSet that remembers keys for ttl.
func New(ttl time.Duration) *TTLSet {
	return &TTLSet{ttl: ttl, now: time.Now, exp: make(map[string]time.Time)}
}

// Add records key and reports whether it was newly added (true = not seen within
// the TTL). A key already present and unexpired returns false.
func (s *TTLSet) Add(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if e, ok := s.exp[key]; ok && now.Before(e) {
		return false
	}
	s.exp[key] = now.Add(s.ttl)
	s.sweep(now)
	return true
}

// Remove forgets key (e.g. when a send failed, so a redelivery can retry).
func (s *TTLSet) Remove(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.exp, key)
}

// Len returns the number of tracked keys (including not-yet-swept expired ones).
func (s *TTLSet) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.exp)
}

// sweep deletes expired keys periodically (caller holds the lock).
func (s *TTLSet) sweep(now time.Time) {
	if s.ops++; s.ops < sweepEvery {
		return
	}
	s.ops = 0
	for k, e := range s.exp {
		if !now.Before(e) {
			delete(s.exp, k)
		}
	}
}
