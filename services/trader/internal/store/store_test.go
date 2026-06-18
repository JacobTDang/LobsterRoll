package store

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestClaim_OncePerProposal(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	first, err := s.Claim(ctx, "p1")
	if err != nil || !first {
		t.Fatalf("first claim = %v err=%v, want true", first, err)
	}
	again, err := s.Claim(ctx, "p1")
	if err != nil || again {
		t.Fatalf("second claim = %v err=%v, want false (idempotent)", again, err)
	}
	if other, _ := s.Claim(ctx, "p2"); !other {
		t.Fatal("different proposal should claim")
	}
}

func TestClaim_PersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "t.db")
	s1, _ := Open(ctx, path)
	if ok, _ := s1.Claim(ctx, "p1"); !ok {
		t.Fatal("first claim")
	}
	s1.Close()

	s2, _ := Open(ctx, path)
	defer s2.Close()
	if ok, _ := s2.Claim(ctx, "p1"); ok {
		t.Fatal("claim must persist across restart (no double-place)")
	}
}

func TestUnclaim_AllowsRetry(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if ok, _ := s.Claim(ctx, "p1"); !ok {
		t.Fatal("claim")
	}
	if ok, _ := s.Claim(ctx, "p1"); ok {
		t.Fatal("second claim should fail before unclaim")
	}
	if err := s.Unclaim(ctx, "p1"); err != nil {
		t.Fatalf("Unclaim: %v", err)
	}
	if ok, _ := s.Claim(ctx, "p1"); !ok {
		t.Fatal("claim should succeed after unclaim (retryable)")
	}
}

func TestUnclaim_KeepsCompleted(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	_, _ = s.Claim(ctx, "p1")
	_ = s.MarkResult(ctx, "p1", "ord-1", "matched")
	// Unclaim must NOT remove a completed placement (only 'claimed' rows).
	if err := s.Unclaim(ctx, "p1"); err != nil {
		t.Fatalf("Unclaim: %v", err)
	}
	if ok, _ := s.Claim(ctx, "p1"); ok {
		t.Fatal("completed placement must not be re-claimable")
	}
}

func TestCapsLedger_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if _, _, _, ok, err := s.LoadCaps(ctx); err != nil || ok {
		t.Fatalf("fresh LoadCaps ok=%v err=%v, want not-ok", ok, err)
	}
	if err := s.SaveCaps(ctx, "2026-06-18", 120.5, 80.25); err != nil {
		t.Fatalf("SaveCaps: %v", err)
	}
	if err := s.SaveCaps(ctx, "2026-06-18", 140.0, 95.0); err != nil { // upsert
		t.Fatalf("SaveCaps upsert: %v", err)
	}
	dk, ds, oe, ok, err := s.LoadCaps(ctx)
	if err != nil || !ok || dk != "2026-06-18" || ds != 140.0 || oe != 95.0 {
		t.Fatalf("LoadCaps = %s/%v/%v ok=%v err=%v", dk, ds, oe, ok, err)
	}
}

func TestClaim_RaceSingleWinner(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	var winners int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := s.Claim(ctx, "hot"); ok {
				atomic.AddInt32(&winners, 1)
			}
		}()
	}
	wg.Wait()
	if winners != 1 {
		t.Fatalf("winners = %d, want exactly 1", winners)
	}
}
