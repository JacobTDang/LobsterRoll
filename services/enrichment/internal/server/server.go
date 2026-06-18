// Package server implements the Enrichment gRPC service: cache-through tokenId
// resolution with single-flight to collapse concurrent misses for the same token.
package server

import (
	"context"
	"errors"
	"log/slog"

	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/services/enrichment/internal/client"
)

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
}

// New returns a Server. If log is nil, slog.Default() is used.
func New(cache Cache, resolver Resolver, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{cache: cache, resolver: resolver, log: log}
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

	// Collapse concurrent misses for the same token into one upstream fetch.
	v, err, _ := s.group.Do(tokenID, func() (any, error) {
		e, ok, ferr := s.resolver.Fetch(ctx, tokenID)
		if ferr != nil {
			return nil, ferr
		}
		if !ok {
			return nil, errNotFound
		}
		// A cache-write hiccup must not discard a good resolution: log and serve it.
		if perr := s.cache.Put(ctx, tokenID, e); perr != nil {
			s.log.Warn("enrichment cache write failed; serving uncached", "token", tokenID, "err", perr)
		}
		return e, nil
	})
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, status.Errorf(codes.NotFound, "token %s not found", tokenID)
		}
		return nil, status.Errorf(codes.Unavailable, "resolve: %v", err)
	}
	return toResponse(v.(client.Enrichment)), nil
}

var errNotFound = errors.New("enrichment: token not found")

func toResponse(e client.Enrichment) *lobsterrollv1.EnrichTokenResponse {
	return &lobsterrollv1.EnrichTokenResponse{
		MarketQuestion: e.MarketQuestion,
		Outcome:        e.Outcome,
		MarketSlug:     e.MarketSlug,
		ConditionId:    e.ConditionID,
	}
}
