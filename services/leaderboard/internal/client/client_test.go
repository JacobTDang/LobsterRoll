package client

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLeaderboard(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "profit_7d.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	entries, err := ParseLeaderboard(data)
	if err != nil {
		t.Fatalf("ParseLeaderboard: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("len = %d, want 5", len(entries))
	}
	// API returns entries sorted by amount descending; preserve that order.
	if got := entries[0].Wallet; got != "0x96cfcb0c30942cfcd1cdf76c7d408794d66b1acb" {
		t.Errorf("entries[0].Wallet = %q", got)
	}
	if got := entries[0].Amount; got != 9238344.623225637 {
		t.Errorf("entries[0].Amount = %v", got)
	}
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Amount < entries[i].Amount {
			t.Errorf("entries not sorted desc at %d: %v < %v", i, entries[i-1].Amount, entries[i].Amount)
		}
	}
}

func TestParseLeaderboard_Errors(t *testing.T) {
	if _, err := ParseLeaderboard([]byte("not json")); err == nil {
		t.Error("expected error for malformed json")
	}
	entries, err := ParseLeaderboard([]byte("[]"))
	if err != nil {
		t.Fatalf("empty array: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("empty array -> %d entries", len(entries))
	}
}
