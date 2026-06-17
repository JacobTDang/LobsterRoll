// Package dedup tracks which (txHash, logIndex) pairs the watcher has already
// processed, so the backfill and live subscription never emit a trade twice.
package dedup

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

type key struct {
	tx  common.Hash
	idx uint
}

// Seen is a concurrent set of processed log identities, with block numbers
// retained so old entries can be pruned once they're safely behind head.
type Seen struct {
	mu    sync.Mutex
	block map[key]uint64
}

// New returns an empty Seen.
func New() *Seen {
	return &Seen{block: make(map[key]uint64)}
}

// Mark records (tx, idx) at the given block and reports whether it had already
// been seen. The first call for a pair returns false; subsequent calls true.
func (s *Seen) Mark(tx common.Hash, idx uint, block uint64) (already bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key{tx, idx}
	if _, ok := s.block[k]; ok {
		return true
	}
	s.block[k] = block
	return false
}

// PruneBelow drops entries from blocks strictly below the given block. Call this
// once a block is deep enough that no reorg/backfill will revisit it.
func (s *Seen) PruneBelow(block uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, b := range s.block {
		if b < block {
			delete(s.block, k)
		}
	}
}

// Len returns the number of tracked entries.
func (s *Seen) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.block)
}
