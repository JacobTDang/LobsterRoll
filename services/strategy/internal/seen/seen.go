// Package seen tracks already-processed source trades so strategy emits at most
// one proposal per trade even if the bus redelivers. Memory is bounded via a
// two-generation rotation: when the current generation fills, it becomes the
// "old" generation and a fresh one starts, so a key stays remembered for
// between max and 2*max subsequent distinct keys.
package seen

import "sync"

const defaultMaxSize = 200_000

// Set is a concurrent, bounded string set.
type Set struct {
	mu  sync.Mutex
	cur map[string]struct{}
	old map[string]struct{}
	max int
}

// New returns a Set with the default capacity.
func New() *Set { return NewSized(defaultMaxSize) }

// NewSized returns a Set that retains at least max recent keys (up to 2*max).
func NewSized(max int) *Set {
	if max < 1 {
		max = 1
	}
	return &Set{cur: make(map[string]struct{}), old: make(map[string]struct{}), max: max}
}

// Add records key and reports whether it was newly added (true = first time).
func (s *Set) Add(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.cur[key]; ok {
		return false
	}
	if _, ok := s.old[key]; ok {
		return false
	}
	s.cur[key] = struct{}{}
	if len(s.cur) >= s.max {
		s.old = s.cur
		s.cur = make(map[string]struct{})
	}
	return true
}

// Len returns the number of tracked keys (bounded by 2*max).
func (s *Set) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cur) + len(s.old)
}
