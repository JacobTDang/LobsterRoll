package cache

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/JacobTDang/LobsterRoll/services/enrichment/internal/client"
)

func openTemp(t *testing.T) *Cache {
	t.Helper()
	c, err := Open(context.Background(), filepath.Join(t.TempDir(), "c.db"), time.Hour)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestCache_MissThenHit(t *testing.T) {
	ctx := context.Background()
	c := openTemp(t)

	if _, hit, err := c.Get(ctx, "tok1"); err != nil || hit {
		t.Fatalf("fresh Get: hit=%v err=%v, want miss", hit, err)
	}

	want := client.Enrichment{MarketQuestion: "Q?", Outcome: "Yes", MarketSlug: "q-slug", ConditionID: "0xc"}
	if err := c.Put(ctx, "tok1", want); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, hit, err := c.Get(ctx, "tok1")
	if err != nil || !hit {
		t.Fatalf("Get after Put: hit=%v err=%v", hit, err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestCache_Replace(t *testing.T) {
	ctx := context.Background()
	c := openTemp(t)
	_ = c.Put(ctx, "t", client.Enrichment{MarketQuestion: "old", Outcome: "No"})
	if err := c.Put(ctx, "t", client.Enrichment{MarketQuestion: "new", Outcome: "Yes"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, _, _ := c.Get(ctx, "t")
	if got.MarketQuestion != "new" || got.Outcome != "Yes" {
		t.Fatalf("got %+v, want new/Yes", got)
	}
}

func TestCache_TTLExpiry(t *testing.T) {
	ctx := context.Background()
	c := openTemp(t)
	c.ttl = time.Hour
	base := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return base }

	if err := c.Put(ctx, "t", client.Enrichment{MarketQuestion: "Q", EndDateUnix: 42}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Within TTL -> hit.
	c.now = func() time.Time { return base.Add(59 * time.Minute) }
	if _, hit, err := c.Get(ctx, "t"); err != nil || !hit {
		t.Fatalf("within TTL: hit=%v err=%v, want hit", hit, err)
	}
	// Past TTL -> miss (so the caller re-fetches the stale row).
	c.now = func() time.Time { return base.Add(2 * time.Hour) }
	if _, hit, err := c.Get(ctx, "t"); err != nil || hit {
		t.Fatalf("past TTL: hit=%v err=%v, want miss", hit, err)
	}
}

func TestCache_TTLDisabled(t *testing.T) {
	ctx := context.Background()
	c, err := Open(ctx, filepath.Join(t.TempDir(), "c.db"), 0) // 0 = never expire
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	base := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return base }
	_ = c.Put(ctx, "t", client.Enrichment{MarketQuestion: "Q"})
	c.now = func() time.Time { return base.Add(1000 * time.Hour) }
	if _, hit, err := c.Get(ctx, "t"); err != nil || !hit {
		t.Fatalf("ttl=0 should never expire: hit=%v err=%v", hit, err)
	}
}

func TestCache_Race(t *testing.T) {
	ctx := context.Background()
	c := openTemp(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			tok := fmt.Sprintf("tok%d", n)
			for j := 0; j < 100; j++ {
				_ = c.Put(ctx, tok, client.Enrichment{MarketQuestion: "Q", Outcome: "Yes"})
				_, _, _ = c.Get(ctx, tok)
			}
		}(i)
	}
	wg.Wait()
}
