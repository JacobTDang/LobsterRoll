package store

import (
	"context"
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestLastProcessedBlock(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)

	if _, ok, err := s.LastProcessedBlock(ctx); err != nil || ok {
		t.Fatalf("fresh: ok=%v err=%v, want ok=false", ok, err)
	}

	for _, want := range []uint64{100, 88641258} {
		if err := s.SetLastProcessedBlock(ctx, want); err != nil {
			t.Fatalf("Set(%d): %v", want, err)
		}
		got, ok, err := s.LastProcessedBlock(ctx)
		if err != nil || !ok || got != want {
			t.Fatalf("got=%d ok=%v err=%v, want %d/true", got, ok, err, want)
		}
	}
}

func TestLastProcessedBlock_Persists(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "w.db")

	s1, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s1.SetLastProcessedBlock(ctx, 4242); err != nil {
		t.Fatalf("Set: %v", err)
	}
	s1.Close()

	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	got, ok, err := s2.LastProcessedBlock(ctx)
	if err != nil || !ok || got != 4242 {
		t.Fatalf("after reopen got=%d ok=%v err=%v, want 4242/true", got, ok, err)
	}
}
