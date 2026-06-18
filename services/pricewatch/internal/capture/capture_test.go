package capture

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type fakePricer struct {
	mid float64
	err error
	mu  sync.Mutex
	got []string
}

func (f *fakePricer) Midpoint(_ context.Context, tok string) (float64, error) {
	f.mu.Lock()
	f.got = append(f.got, tok)
	f.mu.Unlock()
	return f.mid, f.err
}

type fakeStore struct {
	mu   sync.Mutex
	puts map[string]float64
}

func (s *fakeStore) Put(_ context.Context, tok string, _ int64, mid float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.puts == nil {
		s.puts = map[string]float64{}
	}
	s.puts[tok] = mid
	return nil
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestPoll_SnapshotsTrackedTokens(t *testing.T) {
	p := &fakePricer{mid: 0.61}
	st := &fakeStore{}
	tr := New(p, st, time.Hour, quiet())
	tr.Track("a", 1000)
	tr.Track("b", 1000)

	tr.Poll(context.Background(), 1000)

	if len(st.puts) != 2 || st.puts["a"] != 0.61 || st.puts["b"] != 0.61 {
		t.Fatalf("puts = %v, want a&b at 0.61", st.puts)
	}
}

func TestPoll_PrunesStaleTokens(t *testing.T) {
	p := &fakePricer{mid: 0.5}
	tr := New(p, &fakeStore{}, time.Hour, quiet())
	tr.Track("old", 1000)
	tr.Track("fresh", 5000)

	// Poll well after "old" went stale (>1h since ts=1000) but "fresh" is recent.
	tr.Poll(context.Background(), 1000+3700)

	if tr.Active() != 1 {
		t.Fatalf("Active = %d, want 1 (stale token pruned)", tr.Active())
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, tok := range p.got {
		if tok == "old" {
			t.Error("stale token should not have been polled")
		}
	}
}

func TestPoll_FetchErrorSkips(t *testing.T) {
	p := &fakePricer{err: errors.New("clob down")}
	st := &fakeStore{}
	tr := New(p, st, time.Hour, quiet())
	tr.Track("a", 1000)

	tr.Poll(context.Background(), 1000) // must not panic; nothing stored
	if len(st.puts) != 0 {
		t.Errorf("puts = %v, want none (fetch failed)", st.puts)
	}
}

func TestTrack_IgnoresEmpty(t *testing.T) {
	tr := New(&fakePricer{}, &fakeStore{}, time.Hour, quiet())
	tr.Track("", 1000)
	if tr.Active() != 0 {
		t.Errorf("Active = %d, want 0", tr.Active())
	}
}
