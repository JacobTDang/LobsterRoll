// Package seen tracks already-processed source trades so strategy emits at most
// one proposal per trade even if the bus redelivers.
package seen

import "sync"

// Set is a concurrent string set.
type Set struct {
	mu sync.Mutex
	m  map[string]struct{}
}

// New returns an empty Set.
func New() *Set { return &Set{m: make(map[string]struct{})} }

// Add records key and reports whether it was newly added (true = first time).
func (s *Set) Add(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[key]; ok {
		return false
	}
	s.m[key] = struct{}{}
	return true
}

// Len returns the number of tracked keys.
func (s *Set) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m)
}
