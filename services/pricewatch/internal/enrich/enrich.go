// Package enrich resolves a token's market end date from enrichment-svc, cached
// per token (the end date is effectively immutable, and enrichment has its own
// upstream cache).
package enrich

import (
	"context"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
)

// Enricher is the subset of the generated EnrichmentClient we use.
type Enricher interface {
	EnrichToken(ctx context.Context, in *lobsterrollv1.EnrichTokenRequest, opts ...grpc.CallOption) (*lobsterrollv1.EnrichTokenResponse, error)
}

// Client caches token -> end-date unix.
type Client struct {
	c     Enricher
	mu    sync.RWMutex
	cache map[string]int64
}

// New wraps an EnrichmentClient.
func New(c Enricher) *Client { return &Client{c: c, cache: make(map[string]int64)} }

// EndDate returns the token's market end (unix seconds), 0 if unknown. A NotFound
// is treated as "unknown, not cached" so it can resolve later once gamma knows
// the market; transient errors propagate so the caller retries.
func (e *Client) EndDate(ctx context.Context, tokenID string) (int64, error) {
	e.mu.RLock()
	v, ok := e.cache[tokenID]
	e.mu.RUnlock()
	if ok {
		return v, nil
	}
	resp, err := e.c.EnrichToken(ctx, &lobsterrollv1.EnrichTokenRequest{TokenId: tokenID})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return 0, nil
		}
		return 0, err
	}
	end := resp.GetEndDateUnix()
	e.mu.Lock()
	e.cache[tokenID] = end
	e.mu.Unlock()
	return end, nil
}
