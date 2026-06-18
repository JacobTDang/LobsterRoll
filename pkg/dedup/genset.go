package dedup

import "sync"

const defaultGenSize = 200_000

// GenSet is a concurrent, count-bounded string set. Memory is bounded via a
// two-generation rotation: when the current generation fills, it becomes the
// "old" generation and a fresh one starts, so a key stays remembered for between
// max and 2*max subsequent distinct keys. Unlike TTLSet it needs no clock, which
// suits dedup where elapsed time is irrelevant (e.g. one proposal per trade).
type GenSet struct {
	mu  sync.Mutex
	cur map[string]struct{}
	old map[string]struct{}
	max int
}

// NewGen returns a GenSet with the default capacity.
func NewGen() *GenSet { return newGenSized(defaultGenSize) }

// newGenSized returns a GenSet retaining at least max recent keys (up to 2*max).
func newGenSized(max int) *GenSet {
	if max < 1 {
		max = 1
	}
	return &GenSet{cur: make(map[string]struct{}), old: make(map[string]struct{}), max: max}
}

// Add records key and reports whether it was newly added (true = first time).
func (s *GenSet) Add(key string) bool {
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
func (s *GenSet) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cur) + len(s.old)
}
