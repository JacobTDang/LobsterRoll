package syncer

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/client"
	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/store"
)

type fakeFetcher struct {
	mu      sync.Mutex
	wallets []string
	err     error
	calls   int
}

func (f *fakeFetcher) Fetch(context.Context, client.Metric, client.Window, int) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.wallets, f.err
}

func (f *fakeFetcher) set(w []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wallets = w
}

type recordingBroadcaster struct {
	mu      sync.Mutex
	added   [][]string
	removed [][]string
}

func (b *recordingBroadcaster) Broadcast(added, removed []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.added = append(b.added, added)
	b.removed = append(b.removed, removed)
}

func (b *recordingBroadcaster) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.added)
}

func newStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSyncOnce_BroadcastsOnChangeOnly(t *testing.T) {
	ctx := context.Background()
	f := &fakeFetcher{wallets: []string{"0xa", "0xb"}}
	st := newStore(t)
	bc := &recordingBroadcaster{}
	s := New(f, st, bc, client.MetricPNL, "30d", 25, time.Hour, quietLogger())

	// First sync: everything is new -> one broadcast with both added.
	if err := s.syncOnce(ctx); err != nil {
		t.Fatalf("syncOnce: %v", err)
	}
	if bc.count() != 1 {
		t.Fatalf("broadcasts after first sync = %d, want 1", bc.count())
	}

	// Second sync, identical data -> no broadcast.
	if err := s.syncOnce(ctx); err != nil {
		t.Fatalf("syncOnce: %v", err)
	}
	if bc.count() != 1 {
		t.Fatalf("broadcasts after no-op sync = %d, want 1", bc.count())
	}

	// Change the leaderboard -> a second broadcast.
	f.set([]string{"0xb", "0xc"})
	if err := s.syncOnce(ctx); err != nil {
		t.Fatalf("syncOnce: %v", err)
	}
	if bc.count() != 2 {
		t.Fatalf("broadcasts after change = %d, want 2", bc.count())
	}

	got, _ := st.List(ctx)
	if len(got) != 2 || got[0] != "0xb" || got[1] != "0xc" {
		t.Fatalf("watchset = %v, want [0xb 0xc]", got)
	}
}

func TestSyncOnce_FetchError(t *testing.T) {
	f := &fakeFetcher{err: errors.New("boom")}
	s := New(f, newStore(t), &recordingBroadcaster{}, client.MetricPNL, "30d", 25, time.Hour, quietLogger())
	if err := s.syncOnce(context.Background()); err == nil {
		t.Fatal("expected fetch error to propagate")
	}
}

func TestRun_ImmediateSyncThenStops(t *testing.T) {
	f := &fakeFetcher{wallets: []string{"0xa"}}
	bc := &recordingBroadcaster{}
	s := New(f, newStore(t), bc, client.MetricPNL, "30d", 25, time.Hour, quietLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	// Run should perform an immediate sync without waiting a full interval.
	deadline := time.Now().Add(2 * time.Second)
	for bc.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if bc.count() == 0 {
		t.Fatal("Run did not perform an immediate sync")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil on cancel", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
