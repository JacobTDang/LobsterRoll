package dedup

import (
	"testing"
	"time"
)

func TestTTLSet_DedupsWithinTTL(t *testing.T) {
	s := New(24 * time.Hour)
	base := time.Unix(1_700_000_000, 0)
	s.now = func() time.Time { return base }

	if !s.Add("k") {
		t.Fatal("first Add should be new")
	}
	if s.Add("k") {
		t.Fatal("second Add within TTL should be a duplicate")
	}
	if !s.Add("other") {
		t.Fatal("distinct key should be new")
	}
}

func TestTTLSet_ExpiresAfterTTL(t *testing.T) {
	s := New(24 * time.Hour)
	now := time.Unix(1_700_000_000, 0)
	s.now = func() time.Time { return now }

	s.Add("k")
	now = now.Add(23 * time.Hour) // still within TTL
	if s.Add("k") {
		t.Fatal("within 24h should still dedup")
	}
	now = now.Add(2 * time.Hour) // now > 24h since first add
	if !s.Add("k") {
		t.Fatal("after TTL the key should be addable again")
	}
}

func TestTTLSet_RemoveAllowsReadd(t *testing.T) {
	s := New(time.Hour)
	s.Add("k")
	s.Remove("k")
	if !s.Add("k") {
		t.Fatal("after Remove the key should be addable again")
	}
}

func TestTTLSet_SweepBoundsMemory(t *testing.T) {
	s := New(time.Hour)
	now := time.Unix(0, 0)
	s.now = func() time.Time { return now }
	// Add many keys that all expire, then advance and add enough to trigger sweeps.
	for i := 0; i < sweepEvery; i++ {
		s.Add(key(i))
	}
	now = now.Add(2 * time.Hour) // expire them all
	for i := sweepEvery; i < 2*sweepEvery+1; i++ {
		s.Add(key(i))
	}
	// After the sweep, the expired first batch is gone — only the still-valid
	// second batch remains (not the ~2*sweepEvery total ever added).
	if s.Len() > sweepEvery+1 {
		t.Fatalf("Len = %d, expired first batch not swept", s.Len())
	}
}

func key(i int) string { return time.Unix(int64(i), 0).String() }
