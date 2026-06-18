// Package halt holds the trader's kill-switch state, driven by control.halt.
package halt

import "sync/atomic"

// State is a concurrency-safe halt flag.
type State struct {
	halted atomic.Bool
}

// New returns a State (not halted).
func New() *State { return &State{} }

// Set updates the halt state.
func (s *State) Set(halted bool) { s.halted.Store(halted) }

// Halted reports whether execution is currently halted.
func (s *State) Halted() bool { return s.halted.Load() }
