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
