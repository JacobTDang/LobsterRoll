package chain

import (
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

// TestOrderFilledTopics verifies the topic constants are the real keccak256 of
// the event signatures (verified against live Polymarket contracts), so a
// typo can never silently mis-filter the watcher.
func TestOrderFilledTopics(t *testing.T) {
	cases := []struct {
		name string
		sig  string
		want string
	}{
		{
			name: "current",
			sig:  "OrderFilled(bytes32,address,address,uint8,uint256,uint256,uint256,uint256,bytes32,bytes32)",
			want: OrderFilledTopic,
		},
		{
			name: "legacy",
			sig:  "OrderFilled(bytes32,address,address,uint256,uint256,uint256,uint256,uint256)",
			want: OrderFilledTopicLegacy,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := crypto.Keccak256Hash([]byte(c.sig)).Hex()
			if got != c.want {
				t.Fatalf("topic for %s = %s, want %s", c.sig, got, c.want)
			}
		})
	}
}

func TestWatchedExchanges(t *testing.T) {
	got := WatchedExchanges()
	if len(got) != 3 {
		t.Fatalf("expected 3 exchange addresses, got %d: %v", len(got), got)
	}
	// Every watched address must be a valid, normalized (lowercase) address so it
	// compares cleanly against normalized maker/taker addresses.
	for _, a := range got {
		if n, ok := NormalizeAddress(a); !ok || n != a {
			t.Errorf("watched exchange %q is not a valid normalized address (normalized=%q ok=%v)", a, n, ok)
		}
	}
}

func TestNewABIExchanges(t *testing.T) {
	got := NewABIExchanges()
	if len(got) != 2 {
		t.Fatalf("expected 2 new-ABI exchanges, got %d: %v", len(got), got)
	}
	for _, a := range got {
		if a == CTFExchangeLegacy {
			t.Errorf("legacy exchange %q must not be in NewABIExchanges", a)
		}
	}
}
