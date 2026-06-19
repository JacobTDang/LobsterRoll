// Package server implements the Enrichment gRPC service: cache-through tokenId
// resolution with single-flight to collapse concurrent misses for the same token.
package server

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/services/enrichment/internal/client"
)

// negCacheTTL bounds how long an unknown token is remembered as NotFound, so
// repeated lookups for a token gamma doesn't know don't re-hit the upstream every
// time (singleflight only collapses CONCURRENT misses). Short enough that a token
// gamma later learns about still resolves.
const negCacheTTL = time.Hour

// negSweepThreshold triggers a sweep of expired negative-cache entries, bounding
// memory when many distinct unknown tokens are looked up over the process lifetime.
const negSweepThreshold = 1024

// Cache is the persistent enrichment cache.
type Cache interface {
	Get(ctx context.Context, tokenID string) (client.Enrichment, bool, error)
	Put(ctx context.Context, tokenID string, e client.Enrichment) error
}

// Resolver fetches an enrichment from the upstream API. ok=false means the
// token is unknown (not an error).
type Resolver interface {
	Fetch(ctx context.Context, tokenID string) (client.Enrichment, bool, error)
}

// Server implements lobsterrollv1.EnrichmentServer.
type Server struct {
	lobsterrollv1.UnimplementedEnrichmentServer

	cache    Cache
	resolver Resolver
	log      *slog.Logger
	group    singleflight.Group

	now    func() time.Time
	negMu  sync.Mutex
	negTok map[string]time.Time // tokenID -> NotFound-cache expiry
}

// New returns a Server. If log is nil, slog.Default() is used.
func New(cache Cache, resolver Resolver, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{cache: cache, resolver: resolver, log: log, now: time.Now, negTok: make(map[string]time.Time)}
}

// EnrichToken resolves a tokenId to its market/outcome, serving from cache when
// possible and otherwise fetching upstream (deduped via single-flight) and
// caching the result.
func (s *Server) EnrichToken(ctx context.Context, req *lobsterrollv1.EnrichTokenRequest) (*lobsterrollv1.EnrichTokenResponse, error) {
	tokenID := req.GetTokenId()
	if tokenID == "" {
		return nil, status.Error(codes.InvalidArgument, "token_id is required")
	}

	if e, hit, err := s.cache.Get(ctx, tokenID); err != nil {
		return nil, status.Errorf(codes.Internal, "cache get: %v", err)
	} else if hit {
		return toResponse(e), nil
	}
	if s.negativelyCached(tokenID) {
		return nil, status.Errorf(codes.NotFound, "token %s not found (cached)", tokenID)
	}

	// Collapse concurrent misses for the same token into one upstream fetch. The
	// shared fetch runs under a context decoupled from this caller's ctx (with our
	// own timeout) so that if the leader caller cancels, the fetch — and every
	// other waiter coalesced onto it — isn't cancelled along with it.
	v, err, _ := s.group.Do(tokenID, func() (any, error) {
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fetchTimeout)
		defer cancel()
		e, ok, ferr := s.resolver.Fetch(fctx, tokenID)
		if ferr != nil {
			return nil, ferr
		}
		if !ok {
			return nil, errNotFound
		}
		// A cache-write hiccup must not discard a good resolution: log and serve it.
		if perr := s.cache.Put(fctx, tokenID, e); perr != nil {
			s.log.Warn("enrichment cache write failed; serving uncached", "token", tokenID, "err", perr)
		}
		return e, nil
	})
	if err != nil {
		if errors.Is(err, errNotFound) {
			s.cacheNegative(tokenID)
			return nil, status.Errorf(codes.NotFound, "token %s not found", tokenID)
		}
		return nil, status.Errorf(codes.Unavailable, "resolve: %v", err)
	}
	return toResponse(v.(client.Enrichment)), nil
}

// negativelyCached reports whether tokenID is a non-expired NotFound.
func (s *Server) negativelyCached(tokenID string) bool {
	s.negMu.Lock()
	defer s.negMu.Unlock()
	exp, ok := s.negTok[tokenID]
	if !ok {
		return false
	}
	if !s.now().Before(exp) {
		delete(s.negTok, tokenID) // expired
		return false
	}
	return true
}

// cacheNegative remembers tokenID as NotFound for negCacheTTL, sweeping expired
// entries when the map grows large so one-shot unknown tokens can't leak forever.
func (s *Server) cacheNegative(tokenID string) {
	s.negMu.Lock()
	defer s.negMu.Unlock()
	now := s.now()
	if len(s.negTok) >= negSweepThreshold {
		for k, exp := range s.negTok {
			if !now.Before(exp) {
				delete(s.negTok, k)
			}
		}
	}
	s.negTok[tokenID] = now.Add(negCacheTTL)
}

var errNotFound = errors.New("enrichment: token not found")

// fetchTimeout bounds the decoupled upstream fetch.
const fetchTimeout = 20 * time.Second

func toResponse(e client.Enrichment) *lobsterrollv1.EnrichTokenResponse {
	return &lobsterrollv1.EnrichTokenResponse{
		MarketQuestion: e.MarketQuestion,
		Outcome:        e.Outcome,
		MarketSlug:     e.MarketSlug,
		ConditionId:    e.ConditionID,
		EndDateUnix:    e.EndDateUnix,
	}
}
