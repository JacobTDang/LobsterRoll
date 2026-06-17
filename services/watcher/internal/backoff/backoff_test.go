package backoff

import (
	"testing"
	"time"
)

func TestDelay(t *testing.T) {
	base := 1 * time.Second
	max := 30 * time.Second
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{-1, base},
		{0, base},
		{1, 2 * time.Second},
		{2, 4 * time.Second},
		{3, 8 * time.Second},
		{4, 16 * time.Second},
		{5, max},  // 32s -> capped
		{50, max}, // far past cap, no overflow
	}
	for _, tt := range tests {
		if got := Delay(tt.attempt, base, max); got != tt.want {
			t.Errorf("Delay(%d) = %s, want %s", tt.attempt, got, tt.want)
		}
	}
}
