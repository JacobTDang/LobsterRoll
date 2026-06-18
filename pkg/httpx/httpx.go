// Package httpx is a tiny HTTP GET helper with a bounded retry/backoff loop,
// shared by the clients that read Polymarket's lb-api and data-api.
package httpx

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	maxAttempts   = 3
	errBodyMaxLen = 256
)

// backoffSchedule is the wait before each retry (short so tests stay fast).
var backoffSchedule = []time.Duration{100 * time.Millisecond, 200 * time.Millisecond}

// Get performs a GET against url with the given User-Agent (the default Go UA is
// blocked by some WAFs), reading at most bodyLimit bytes. It retries on network
// errors and transient statuses (429 or >=500) up to a small fixed cap; any
// other non-200 status fails immediately. ctx cancellation is honored in backoff.
func Get(ctx context.Context, hc *http.Client, url, userAgent string, bodyLimit int64) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			d := backoffSchedule[len(backoffSchedule)-1]
			if attempt-1 < len(backoffSchedule) {
				d = backoffSchedule[attempt-1]
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(d):
			}
		}
		body, transient, err := doOnce(ctx, hc, url, userAgent, bodyLimit)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !transient {
			return nil, err
		}
	}
	return nil, fmt.Errorf("giving up after %d attempts: %w", maxAttempts, lastErr)
}

// doOnce issues a single request, returning transient=true on a retryable
// failure (network error, 429, or >=500).
func doOnce(ctx context.Context, hc *http.Client, url, userAgent string, bodyLimit int64) (body []byte, transient bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := hc.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err = io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
	if err != nil {
		return nil, true, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		isTransient := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return nil, isTransient, fmt.Errorf("status %d: %s", resp.StatusCode, truncate(body, errBodyMaxLen))
	}
	return body, false, nil
}

// truncate bounds a response body for safe inclusion in error messages.
func truncate(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + fmt.Sprintf("... (%d bytes truncated)", len(b)-max)
}
