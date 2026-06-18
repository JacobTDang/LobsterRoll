package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStore_PutAndNearest(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)

	for _, sn := range []struct {
		ts  int64
		mid float64
	}{{100, 0.40}, {200, 0.50}, {300, 0.65}} {
		if err := s.Put(ctx, "tok", sn.ts, sn.mid); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	// Nearest to 190 is the ts=200 (0.50) snapshot.
	got, err := s.Nearest(ctx, "tok", 190)
	if err != nil {
		t.Fatalf("Nearest: %v", err)
	}
	if got.TS != 200 || got.Mid != 0.50 {
		t.Errorf("Nearest(190) = %+v, want {200 0.50}", got)
	}
	// Past the last snapshot -> clamps to the latest.
	if got, _ := s.Nearest(ctx, "tok", 99999); got.TS != 300 {
		t.Errorf("Nearest(future) ts = %d, want 300", got.TS)
	}
}

func TestStore_PutIdempotent(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	_ = s.Put(ctx, "tok", 100, 0.40)
	if err := s.Put(ctx, "tok", 100, 0.42); err != nil { // same (token,ts) overwrites
		t.Fatalf("Put overwrite: %v", err)
	}
	got, _ := s.Nearest(ctx, "tok", 100)
	if got.Mid != 0.42 {
		t.Errorf("mid = %v, want 0.42 (overwritten)", got.Mid)
	}
}

func TestStore_NearestMissing(t *testing.T) {
	if _, err := openTemp(t).Nearest(context.Background(), "nope", 100); !errors.Is(err, ErrNoSnapshot) {
		t.Errorf("err = %v, want ErrNoSnapshot", err)
	}
}

func TestStore_Prune(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	_ = s.Put(ctx, "tok", 100, 0.4)
	_ = s.Put(ctx, "tok", 500, 0.6)
	if err := s.Prune(ctx, 300); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if _, err := s.Nearest(ctx, "tok", 100); err != nil {
		t.Fatalf("Nearest after prune: %v", err)
	}
	got, _ := s.Nearest(ctx, "tok", 100) // only ts=500 remains
	if got.TS != 500 {
		t.Errorf("after prune nearest ts = %d, want 500 (old pruned)", got.TS)
	}
}
