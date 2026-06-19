// Package enrich resolves a token's market end date from enrichment-svc, cached
// per token. A positive end date is effectively immutable and cached forever; an
// unknown result (NotFound, or resolved-but-no-end-date-yet) is negatively cached
// for a short TTL so it re-resolves later without re-hitting gamma every pass.
package enrich

import (
	"context"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
)

// negTTL bounds how long an unknown end date is cached before re-resolving — long
// enough to stop re-fetching every settle pass, short enough that gamma filling
// the end date later is still picked up.
const negTTL = 6 * time.Hour

// Enricher is the subset of the generated EnrichmentClient we use.
type Enricher interface {
	EnrichToken(ctx context.Context, in *lobsterrollv1.EnrichTokenRequest, opts ...grpc.CallOption) (*lobsterrollv1.EnrichTokenResponse, error)
}

// entry is a cached end date. A zero exp means it never expires (a known,
// positive end date); a non-zero exp marks a negatively-cached unknown.
type entry struct {
	end int64
	exp time.Time
}

// Client caches token -> end-date unix.
type Client struct {
	c     Enricher
	now   func() time.Time
	mu    sync.RWMutex
	cache map[string]entry
}

// New wraps an EnrichmentClient.
func New(c Enricher) *Client { return &Client{c: c, now: time.Now, cache: make(map[string]entry)} }

// EndDate returns the token's market end (unix seconds), 0 if unknown. Positive
// end dates are cached permanently; unknowns (NotFound or end==0) are cached for
// negTTL so repeated settle passes don't re-hit gamma but a later fill is still
// picked up. Transient errors propagate so the caller retries.
func (e *Client) EndDate(ctx context.Context, tokenID string) (int64, error) {
	e.mu.RLock()
	ent, ok := e.cache[tokenID]
	e.mu.RUnlock()
	if ok && (ent.exp.IsZero() || e.now().Before(ent.exp)) {
		return ent.end, nil
	}

	resp, err := e.c.EnrichToken(ctx, &lobsterrollv1.EnrichTokenRequest{TokenId: tokenID})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			e.store(tokenID, entry{end: 0, exp: e.now().Add(negTTL)})
			return 0, nil
		}
		return 0, err // transient: don't cache, let the caller retry
	}
	end := resp.GetEndDateUnix()
	if end > 0 {
		e.store(tokenID, entry{end: end}) // immutable -> cache forever
	} else {
		e.store(tokenID, entry{end: 0, exp: e.now().Add(negTTL)}) // resolved but no end yet
	}
	return end, nil
}

func (e *Client) store(tokenID string, ent entry) {
	e.mu.Lock()
	e.cache[tokenID] = ent
	e.mu.Unlock()
}
