package watchset

import (
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

const (
	addrA = "0x037c0f46600702e77ccb738721a78d6418d3a458"
	addrB = "0xa6d24a207011c9a5d54fa3a04f3e87365d2e12f4"
)

func TestSet_ApplyHasRemove(t *testing.T) {
	s := New()
	if s.Len() != 0 {
		t.Fatalf("fresh Len = %d", s.Len())
	}

	// Add (mixed case input should match lowercase address).
	s.Apply([]string{"0x037C0F46600702E77CCB738721A78D6418D3A458", addrB}, nil)
	if s.Len() != 2 {
		t.Fatalf("Len = %d, want 2", s.Len())
	}
	if !s.Has(common.HexToAddress(addrA)) {
		t.Error("addrA should be present despite mixed-case input")
	}
	if !s.Has(common.HexToAddress(addrB)) {
		t.Error("addrB should be present")
	}

	// Remove one.
	s.Apply(nil, []string{addrA})
	if s.Has(common.HexToAddress(addrA)) {
		t.Error("addrA should be removed")
	}
	if !s.Has(common.HexToAddress(addrB)) {
		t.Error("addrB should remain")
	}

	// Unknown address is not present.
	if s.Has(common.HexToAddress("0x000000000000000000000000000000000000dead")) {
		t.Error("unknown address should not be present")
	}
}

func TestSet_IgnoresInvalid(t *testing.T) {
	s := New()
	s.Apply([]string{"not-an-address", "0xshort", "", addrA}, nil)
	if s.Len() != 1 {
		t.Fatalf("Len = %d, want 1 (invalid dropped)", s.Len())
	}
}

func TestSet_Race(t *testing.T) {
	s := New()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				s.Apply([]string{addrA}, nil)
				_ = s.Has(common.HexToAddress(addrB))
				s.Apply(nil, []string{addrA})
				_ = s.Len()
			}
		}()
	}
	wg.Wait()
}
