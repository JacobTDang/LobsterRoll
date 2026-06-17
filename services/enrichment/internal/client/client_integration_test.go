//go:build integration

package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// TestFetch_Live resolves a fresh, real tokenId discovered from data-api
// activity. Run with: go test -tags=integration ./services/enrichment/...
func TestFetch_Live(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tokenID, wantTitle, wantOutcome := discoverToken(t, ctx)
	e, ok, err := New(DefaultBaseURL, nil).Fetch(ctx, tokenID)
	if err != nil || !ok {
		t.Fatalf("live Fetch(%s): ok=%v err=%v", tokenID, ok, err)
	}
	if e.MarketQuestion != wantTitle {
		t.Errorf("question = %q, want %q", e.MarketQuestion, wantTitle)
	}
	if e.Outcome != wantOutcome {
		t.Errorf("outcome = %q, want %q", e.Outcome, wantOutcome)
	}
	t.Logf("resolved %s -> %q / %q", tokenID, e.MarketQuestion, e.Outcome)
}

func discoverToken(t *testing.T, ctx context.Context) (tokenID, title, outcome string) {
	t.Helper()
	lb := getJSON[[]map[string]any](t, ctx, "https://lb-api.polymarket.com/volume?window=7d&limit=8")
	for _, w := range lb {
		wallet, _ := w["proxyWallet"].(string)
		act := getJSON[[]map[string]any](t, ctx, "https://data-api.polymarket.com/activity?user="+wallet+"&limit=3")
		for _, a := range act {
			asset, _ := a["asset"].(string)
			ti, _ := a["title"].(string)
			oc, _ := a["outcome"].(string)
			if asset != "" && ti != "" && oc != "" {
				return asset, ti, oc
			}
		}
	}
	t.Skip("could not discover a live token from activity")
	return "", "", ""
}

func getJSON[T any](t *testing.T, ctx context.Context, url string) T {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return v
}
