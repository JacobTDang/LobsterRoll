package server

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
	"github.com/JacobTDang/LobsterRoll/services/enrichment/internal/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type memCache struct {
	mu     sync.Mutex
	m      map[string]client.Enrichment
	putErr error
}

func newMemCache() *memCache { return &memCache{m: map[string]client.Enrichment{}} }
func (c *memCache) Get(_ context.Context, tok string) (client.Enrichment, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[tok]
	return e, ok, nil
}
func (c *memCache) Put(_ context.Context, tok string, e client.Enrichment) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.putErr != nil {
		return c.putErr
	}
	c.m[tok] = e
	return nil
}

// ctxResolver honors its context so we can assert the fetch isn't tied to a
// cancelled caller.
type ctxResolver struct{ result client.Enrichment }

func (r *ctxResolver) Fetch(ctx context.Context, _ string) (client.Enrichment, bool, error) {
	if err := ctx.Err(); err != nil {
		return client.Enrichment{}, false, err
	}
	return r.result, true, nil
}

// A caller cancelling its context must NOT cancel the shared upstream fetch
// (single-flight decouples it via context.WithoutCancel).
func TestEnrichToken_FetchDecoupledFromCallerCancel(t *testing.T) {
	s := New(newMemCache(), &ctxResolver{result: sample}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // leader caller's context already cancelled

	resp, err := s.EnrichToken(ctx, &lobsterrollv1.EnrichTokenRequest{TokenId: "tok"})
	if err != nil {
		t.Fatalf("EnrichToken with cancelled caller ctx = %v, want success (fetch decoupled)", err)
	}
	if resp.GetMarketQuestion() != sample.MarketQuestion {
		t.Fatalf("resp = %+v, want %+v", resp, sample)
	}
}

type fakeResolver struct {
	calls   int32
	delay   time.Duration
	result  client.Enrichment
	found   bool
	failErr error
}

func (r *fakeResolver) Fetch(_ context.Context, _ string) (client.Enrichment, bool, error) {
	atomic.AddInt32(&r.calls, 1)
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	if r.failErr != nil {
		return client.Enrichment{}, false, r.failErr
	}
	return r.result, r.found, nil
}

func dial(t *testing.T, srv *Server) lobsterrollv1.EnrichmentClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	gs := grpc.NewServer()
	lobsterrollv1.RegisterEnrichmentServer(gs, srv)
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return lobsterrollv1.NewEnrichmentClient(conn)
}

var sample = client.Enrichment{MarketQuestion: "Q?", Outcome: "Yes", MarketSlug: "q", ConditionID: "0xc"}

func TestEnrichToken_MissThenCached(t *testing.T) {
	res := &fakeResolver{result: sample, found: true}
	client := dial(t, New(newMemCache(), res, nil))

	// First call: resolver hit, response correct.
	resp, err := client.EnrichToken(context.Background(), &lobsterrollv1.EnrichTokenRequest{TokenId: "t1"})
	if err != nil {
		t.Fatalf("EnrichToken: %v", err)
	}
	if resp.GetMarketQuestion() != "Q?" || resp.GetOutcome() != "Yes" || resp.GetConditionId() != "0xc" {
		t.Fatalf("resp = %+v", resp)
	}
	// Second call: served from cache, resolver NOT called again.
	if _, err := client.EnrichToken(context.Background(), &lobsterrollv1.EnrichTokenRequest{TokenId: "t1"}); err != nil {
		t.Fatalf("EnrichToken 2: %v", err)
	}
	if got := atomic.LoadInt32(&res.calls); got != 1 {
		t.Fatalf("resolver calls = %d, want 1 (second served from cache)", got)
	}
}

func TestEnrichToken_NotFound(t *testing.T) {
	cl := dial(t, New(newMemCache(), &fakeResolver{found: false}, nil))
	_, err := cl.EnrichToken(context.Background(), &lobsterrollv1.EnrichTokenRequest{TokenId: "missing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("err = %v, want NotFound", err)
	}
}

func TestEnrichToken_EmptyToken(t *testing.T) {
	cl := dial(t, New(newMemCache(), &fakeResolver{}, nil))
	_, err := cl.EnrichToken(context.Background(), &lobsterrollv1.EnrichTokenRequest{TokenId: ""})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("err = %v, want InvalidArgument", err)
	}
}

func TestEnrichToken_CacheWriteFailureStillServes(t *testing.T) {
	cache := newMemCache()
	cache.putErr = errors.New("disk full")
	cl := dial(t, New(cache, &fakeResolver{result: sample, found: true}, nil))

	resp, err := cl.EnrichToken(context.Background(), &lobsterrollv1.EnrichTokenRequest{TokenId: "t1"})
	if err != nil {
		t.Fatalf("EnrichToken should succeed despite cache Put failure: %v", err)
	}
	if resp.GetMarketQuestion() != "Q?" {
		t.Fatalf("resp = %+v, want the resolved enrichment", resp)
	}
}

func TestEnrichToken_SingleFlight(t *testing.T) {
	res := &fakeResolver{result: sample, found: true, delay: 100 * time.Millisecond}
	srv := New(newMemCache(), res, nil)

	// Many concurrent misses for the same token must collapse to one fetch.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := srv.EnrichToken(context.Background(), &lobsterrollv1.EnrichTokenRequest{TokenId: "hot"})
			if err != nil {
				t.Errorf("EnrichToken: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&res.calls); got != 1 {
		t.Fatalf("resolver calls = %d, want 1 (single-flight)", got)
	}
}
