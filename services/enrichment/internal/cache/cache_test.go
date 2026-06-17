package cache

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/JacobTDang/LobsterRoll/services/enrichment/internal/client"
)

func openTemp(t *testing.T) *Cache {
	t.Helper()
	c, err := Open(context.Background(), filepath.Join(t.TempDir(), "c.db"))
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
