// Package backoff computes capped exponential reconnect delays.
package backoff

import "time"

// Delay returns base * 2^attempt, clamped to [base, max]. attempt is 0-based
// (attempt 0 -> base). It never returns more than max, and handles overflow.
func Delay(attempt int, base, max time.Duration) time.Duration {
	if attempt <= 0 {
		return base
	}
	d := base
	for i := 0; i < attempt; i++ {
		d *= 2
		if d >= max || d <= 0 { // d<=0 guards against overflow
			return max
		}
	}
	if d > max {
		return max
	}
	return d
}
