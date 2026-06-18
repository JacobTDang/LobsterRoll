package seen

import (
	"fmt"
	"sync"
	"testing"
)

func TestSet_Add(t *testing.T) {
	s := New()
	if !s.Add("a") {
		t.Error("first Add should be true (new)")
	}
	if s.Add("a") {
		t.Error("second Add should be false (seen)")
	}
	if !s.Add("b") {
		t.Error("new key should be true")
	}
	if s.Len() != 2 {
		t.Errorf("Len = %d, want 2", s.Len())
	}
}

func TestSet_Bounded(t *testing.T) {
	s := NewSized(3)
	// Add many distinct keys; Len must never exceed 2*max.
	for i := 0; i < 100; i++ {
		s.Add(fmt.Sprintf("k%d", i))
		if s.Len() > 6 {
			t.Fatalf("Len = %d after %d adds, want <= 6 (2*max)", s.Len(), i+1)
		}
	}
	// A just-added key is still deduped.
	s.Add("recent")
	if s.Add("recent") {
		t.Error("recently added key should be deduped")
	}
}

func TestSet_Race(t *testing.T) {
	s := New()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				s.Add(fmt.Sprintf("k%d-%d", n, j%10))
				_ = s.Len()
			}
		}(i)
	}
	wg.Wait()
}
