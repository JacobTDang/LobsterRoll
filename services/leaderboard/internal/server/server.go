// Package server implements the Leaderboard gRPC service: a snapshot of the
// current watchset (GetWatchset) and a live stream of changes (StreamWatchset).
package server

import (
	"context"
	"sync"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/pkg/chain"
	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Lister provides the current watchset, its last sync time, and per-wallet
// consistency stats. *store.Store satisfies it.
type Lister interface {
	List(ctx context.Context) ([]string, error)
	LastSync(ctx context.Context) (int64, error)
	GetStats(ctx context.Context, wallet string) (store.StatsRecord, bool, error)
}

// subBuffer is the per-subscriber channel depth. Watchset changes are
// infrequent, so a small buffer absorbs transient slow consumers.
const subBuffer = 16

// subscriber holds the per-stream delivery channel plus a lost signal. When a
// subscriber falls too far behind to receive a broadcast, the broadcaster
// removes it and closes lost so the stream can tell the client to re-sync.
type subscriber struct {
	ch   chan *lobsterrollv1.WatchsetUpdate
	lost chan struct{}
}

// Server implements lobsterrollv1.LeaderboardServer.
type Server struct {
	lobsterrollv1.UnimplementedLeaderboardServer

	store Lister

	mu     sync.Mutex
	subs   map[int]*subscriber
	nextID int
}

// New returns a Server backed by the given watchset lister.
func New(store Lister) *Server {
	return &Server{
		store: store,
		subs:  make(map[int]*subscriber),
	}
}

// GetWatchset returns the current set of watched wallets and the last sync time.
func (s *Server) GetWatchset(ctx context.Context, _ *lobsterrollv1.GetWatchsetRequest) (*lobsterrollv1.GetWatchsetResponse, error) {
	wallets, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	lastSync, err := s.store.LastSync(ctx)
	if err != nil {
		return nil, err
	}
	return &lobsterrollv1.GetWatchsetResponse{Wallets: wallets, LastSyncedUnix: lastSync}, nil
}

// GetWalletStats returns our cached consistency stats for a wallet. Found is
// false (and the other fields zero) when we have not computed stats for it yet.
// The request wallet is normalized to match how stats are keyed in the store.
func (s *Server) GetWalletStats(ctx context.Context, req *lobsterrollv1.GetWalletStatsRequest) (*lobsterrollv1.WalletStats, error) {
	wallet, ok := chain.NormalizeAddress(req.GetWallet())
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "invalid wallet %q", req.GetWallet())
	}
	rec, found, err := s.store.GetStats(ctx, wallet)
	if err != nil {
		return nil, err
	}
	if !found {
		return &lobsterrollv1.WalletStats{Wallet: wallet, Found: false}, nil
	}
	return &lobsterrollv1.WalletStats{
		Wallet:          rec.Wallet,
		WinRate:         rec.WinRate,
		ResolvedMarkets: rec.ResolvedMarkets,
		RealizedPnl:     rec.RealizedPnL,
		Profit_30D:      rec.Profit30D,
		PortfolioValue:  rec.PortfolioValue,
		TradedMarkets:   rec.TradedMarkets,
		ComputedUnix:    rec.ComputedUnix,
		Roi:             rec.ROI,
		Found:           true,
	}, nil
}

// StreamWatchset streams the watchset to the client. It subscribes first (to
// avoid a TOCTOU gap where a change between snapshot and subscribe is missed),
// then sends an initial snapshot as the first update (Added = current set),
// then streams subsequent diffs. The snapshot is skipped when the set is empty.
func (s *Server) StreamWatchset(_ *lobsterrollv1.StreamWatchsetRequest, stream lobsterrollv1.Leaderboard_StreamWatchsetServer) error {
	sub, unsubscribe := s.subscribe()
	defer unsubscribe()

	ctx := stream.Context()

	// Subscribe-then-snapshot: the subscription is already live, so any change
	// concurrent with the snapshot is buffered and delivered as a later diff.
	wallets, err := s.store.List(ctx)
	if err != nil {
		return err
	}
	if len(wallets) > 0 {
		if err := stream.Send(&lobsterrollv1.WatchsetUpdate{Added: wallets}); err != nil {
			return err
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-sub.lost:
			return status.Error(codes.ResourceExhausted,
				"watchset stream fell behind; reconnect and re-sync via GetWatchset")
		case upd := <-sub.ch:
			if err := stream.Send(upd); err != nil {
				return err
			}
		}
	}
}

// Broadcast delivers a watchset change to all active stream subscribers. It
// never blocks: if a subscriber's buffer is full, that subscriber is removed
// and its lost channel is closed so the stream can signal the client to
// reconnect and re-sync rather than silently falling out of date.
func (s *Server) Broadcast(added, removed []string) {
	if len(added) == 0 && len(removed) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sub := range s.subs {
		// Per-subscriber copy so subscribers never alias each other or the
		// caller's diff buffers (which the caller may mutate after this call).
		upd := &lobsterrollv1.WatchsetUpdate{
			Added:   copyStrings(added),
			Removed: copyStrings(removed),
		}
		select {
		case sub.ch <- upd:
		default:
			// Subscriber is too far behind: evict it and signal a re-sync.
			delete(s.subs, id)
			close(sub.lost)
		}
	}
}

func copyStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func (s *Server) subscribe() (*subscriber, func()) {
	sub := &subscriber{
		ch:   make(chan *lobsterrollv1.WatchsetUpdate, subBuffer),
		lost: make(chan struct{}),
	}
	s.mu.Lock()
	id := s.nextID
	s.nextID++
	s.subs[id] = sub
	s.mu.Unlock()

	return sub, func() {
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
