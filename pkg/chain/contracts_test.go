package chain

import "testing"

func TestWatchedExchanges(t *testing.T) {
	got := WatchedExchanges()
	if len(got) != 2 {
		t.Fatalf("expected 2 exchange addresses, got %d", len(got))
	}
	for _, a := range got {
		if len(a) != 42 || a[:2] != "0x" {
			t.Errorf("address %q is not a 0x-prefixed 20-byte address", a)
		}
	}
}
