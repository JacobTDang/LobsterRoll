// Package server implements the Leaderboard gRPC service: a snapshot of the
// current watchset (GetWatchset) and a live stream of changes (StreamWatchset).
package server

import (
	"context"
	"sync"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
)

// Lister provides the current watchset. *store.Store satisfies it.
type Lister interface {
	List(ctx context.Context) ([]string, error)
}

// subBuffer is the per-subscriber channel depth. Watchset changes are
// infrequent, so a small buffer absorbs transient slow consumers.
const subBuffer = 16

// Server implements lobsterrollv1.LeaderboardServer.
type Server struct {
	lobsterrollv1.UnimplementedLeaderboardServer

	store Lister

	mu     sync.Mutex
	subs   map[int]chan *lobsterrollv1.WatchsetUpdate
	nextID int
}

// New returns a Server backed by the given watchset lister.
func New(store Lister) *Server {
	return &Server{
		store: store,
		subs:  make(map[int]chan *lobsterrollv1.WatchsetUpdate),
	}
}

// GetWatchset returns the current set of watched wallets.
func (s *Server) GetWatchset(ctx context.Context, _ *lobsterrollv1.GetWatchsetRequest) (*lobsterrollv1.GetWatchsetResponse, error) {
	wallets, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	return &lobsterrollv1.GetWatchsetResponse{Wallets: wallets}, nil
}

// StreamWatchset streams watchset diffs until the client disconnects. Clients
// should call GetWatchset once for the initial snapshot, then consume diffs here.
func (s *Server) StreamWatchset(_ *lobsterrollv1.StreamWatchsetRequest, stream lobsterrollv1.Leaderboard_StreamWatchsetServer) error {
	ch, unsubscribe := s.subscribe()
	defer unsubscribe()

	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case upd := <-ch:
			if err := stream.Send(upd); err != nil {
				return err
			}
		}
	}
}

// Broadcast delivers a watchset change to all active stream subscribers. It
// never blocks: if a subscriber's buffer is full, the update is dropped for
// that subscriber (which can re-sync via GetWatchset).
func (s *Server) Broadcast(added, removed []string) {
	if len(added) == 0 && len(removed) == 0 {
		return
	}
	upd := &lobsterrollv1.WatchsetUpdate{Added: added, Removed: removed}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.subs {
		select {
		case ch <- upd:
		default: // subscriber is behind; drop rather than block the syncer.
		}
	}
}

func (s *Server) subscribe() (<-chan *lobsterrollv1.WatchsetUpdate, func()) {
	ch := make(chan *lobsterrollv1.WatchsetUpdate, subBuffer)
	s.mu.Lock()
	id := s.nextID
	s.nextID++
	s.subs[id] = ch
	s.mu.Unlock()

	return ch, func() {
		s.mu.Lock()
		delete(s.subs, id)
		s.mu.Unlock()
	}
}

func (s *Server) subscriberCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs)
}
