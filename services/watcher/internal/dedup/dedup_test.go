package dedup

import (
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestSeen_Mark(t *testing.T) {
	s := New()
	tx := common.HexToHash("0xabc")

	if s.Mark(tx, 1, 100) {
		t.Error("first Mark should report not-seen")
	}
	if !s.Mark(tx, 1, 100) {
		t.Error("second Mark of same (tx,idx) should report seen")
	}
	// Same tx, different index is distinct.
	if s.Mark(tx, 2, 100) {
		t.Error("different logIndex should be distinct")
	}
	// Different tx, same index is distinct.
	if s.Mark(common.HexToHash("0xdef"), 1, 100) {
		t.Error("different tx should be distinct")
	}
}

func TestSeen_PruneBelow(t *testing.T) {
	s := New()
	s.Mark(common.HexToHash("0x1"), 0, 100)
	s.Mark(common.HexToHash("0x2"), 0, 200)
	s.Mark(common.HexToHash("0x3"), 0, 300)

	s.PruneBelow(250)
	if s.Len() != 1 {
		t.Fatalf("Len after prune = %d, want 1", s.Len())
	}
	// The pruned entry is forgotten, so it reads as not-seen again.
	if s.Mark(common.HexToHash("0x1"), 0, 100) {
		t.Error("pruned entry should read as not-seen")
	}
	// The retained one is still seen.
	if !s.Mark(common.HexToHash("0x3"), 0, 300) {
		t.Error("retained entry should still be seen")
	}
}

func TestSeen_Race(t *testing.T) {
	s := New()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			tx := common.BigToHash(common.Big1)
			for j := 0; j < 200; j++ {
				s.Mark(tx, uint(j), uint64(j))
				if j%50 == 0 {
					s.PruneBelow(uint64(j))
				}
				_ = s.Len()
			}
		}(i)
	}
	wg.Wait()
}
