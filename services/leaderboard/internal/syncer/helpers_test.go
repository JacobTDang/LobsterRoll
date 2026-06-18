package syncer

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"github.com/JacobTDang/LobsterRoll/services/leaderboard/internal/store"
)

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
