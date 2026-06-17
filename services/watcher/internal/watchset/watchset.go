// Package watchset holds the live, thread-safe set of wallets the watcher
// filters on-chain trades against. It is fed by leaderboard-svc's GetWatchset
// snapshot plus StreamWatchset diffs.
package watchset

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"

	"github.com/JacobTDang/LobsterRoll/pkg/chain"
)

// Set is a concurrent set of watched addresses.
type Set struct {
	mu sync.RWMutex
	m  map[common.Address]struct{}
}

// New returns an empty Set.
func New() *Set {
	return &Set{m: make(map[common.Address]struct{})}
}

// Apply adds and removes wallets (hex strings, any case). Invalid addresses are
// ignored rather than corrupting the set.
func (s *Set) Apply(added, removed []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range added {
		if n, ok := chain.NormalizeAddress(a); ok {
			s.m[common.HexToAddress(n)] = struct{}{}
		}
	}
	for _, a := range removed {
		if n, ok := chain.NormalizeAddress(a); ok {
			delete(s.m, common.HexToAddress(n))
		}
	}
}

// Has reports whether addr is currently watched.
func (s *Set) Has(addr common.Address) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.m[addr]
	return ok
}

// Len returns the number of watched wallets.
func (s *Set) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}
