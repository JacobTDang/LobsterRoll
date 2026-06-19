package enrich

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	lobsterrollv1 "github.com/JacobTDang/LobsterRoll/gen/go"
)

type fakeEnricher struct {
	end   int64
	err   error
	calls int
}

func (f *fakeEnricher) EnrichToken(context.Context, *lobsterrollv1.EnrichTokenRequest, ...grpc.CallOption) (*lobsterrollv1.EnrichTokenResponse, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return &lobsterrollv1.EnrichTokenResponse{EndDateUnix: f.end}, nil
}

func TestEndDate_CachesResult(t *testing.T) {
	f := &fakeEnricher{end: 1700}
	c := New(f)
	for i := 0; i < 3; i++ {
		v, err := c.EndDate(context.Background(), "tok")
		if err != nil || v != 1700 {
			t.Fatalf("EndDate = %v, %v; want 1700, nil", v, err)
		}
	}
	if f.calls != 1 {
		t.Errorf("upstream calls = %d, want 1 (cached)", f.calls)
	}
}

func TestEndDate_NotFoundNegativelyCachedThenReResolves(t *testing.T) {
	f := &fakeEnricher{err: status.Error(codes.NotFound, "nope")}
	c := New(f)
	now := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return now }

	if v, err := c.EndDate(context.Background(), "tok"); err != nil || v != 0 {
		t.Fatalf("NotFound EndDate = %v, %v; want 0, nil", v, err)
	}
	// Within negTTL: served from the negative cache, no re-fetch (stops hammering gamma).
	_, _ = c.EndDate(context.Background(), "tok")
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1 (NotFound negatively cached)", f.calls)
	}
	// After negTTL: re-resolves (the market may have become known).
	now = now.Add(negTTL + time.Minute)
	_, _ = c.EndDate(context.Background(), "tok")
	if f.calls != 2 {
		t.Errorf("calls = %d, want 2 (re-resolves after negTTL)", f.calls)
	}
}

func TestEndDate_ZeroEndReResolves(t *testing.T) {
	// A resolved market with no end date yet (end==0) must NOT be cached forever.
	f := &fakeEnricher{end: 0}
	c := New(f)
	now := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return now }
	_, _ = c.EndDate(context.Background(), "tok")
	now = now.Add(negTTL + time.Minute)
	_, _ = c.EndDate(context.Background(), "tok")
	if f.calls != 2 {
		t.Errorf("calls = %d, want 2 (end==0 must re-resolve, not cache forever)", f.calls)
	}
}

func TestEndDate_TransientErrorPropagates(t *testing.T) {
	f := &fakeEnricher{err: status.Error(codes.Unavailable, "down")}
	if _, err := New(f).EndDate(context.Background(), "tok"); err == nil {
		t.Error("transient error should propagate")
	}
}
