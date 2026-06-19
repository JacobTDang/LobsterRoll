package positions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Fetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("user") != "0xme" {
			t.Errorf("user = %q", r.URL.Query().Get("user"))
		}
		_, _ = w.Write([]byte(`[{"asset":"tokA","conditionId":"0xc","oppositeAsset":"tokB","outcome":"Yes","size":100,"curPrice":0.6,"title":"Game","slug":"game"}]`))
	}))
	defer srv.Close()

	ps, err := New(srv.URL, srv.Client()).Fetch(context.Background(), "0xme")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(ps) != 1 || ps[0].Asset != "tokA" || ps[0].OppositeAsset != "tokB" {
		t.Fatalf("positions = %+v", ps)
	}
}

func held() []Position {
	return []Position{
		{Asset: "tokA", OppositeAsset: "tokB", ConditionID: "0xc", Outcome: "Yes", Size: 100, CurPrice: 0.6, Title: "Game", Slug: "game"},
		{Asset: "dust", Size: 0},                                     // dropped (dust)
		{Asset: "resolved", Size: 50, Redeemable: true, CurPrice: 0}, // dropped (resolved/claimable)
	}
}

func TestCache_MatchExactSell(t *testing.T) {
	c := NewCache("0xme")
	c.Replace(held())

	// Whale SELLS the exact outcome we hold -> fire (exiting our outcome).
	h, kind, fire := c.Match("tokA", "sell", "0xwhale")
	if kind != Exact || !fire || h.Title != "Game" {
		t.Fatalf("exact sell: kind=%v fire=%v h=%+v", kind, fire, h)
	}
	// Whale BUYS the same outcome -> Exact but no fire (doubling down, phase 1).
	if _, kind, fire := c.Match("tokA", "buy", "0xwhale"); kind != Exact || fire {
		t.Errorf("exact buy: kind=%v fire=%v, want Exact/false", kind, fire)
	}
}

func TestCache_MatchOppositeBuy(t *testing.T) {
	c := NewCache("0xme")
	c.Replace(held())
	// Whale BUYS the opposite outcome -> fire (betting against us).
	if _, kind, fire := c.Match("tokB", "buy", "0xwhale"); kind != Opposite || !fire {
		t.Errorf("opposite buy: kind=%v fire=%v, want Opposite/true", kind, fire)
	}
	// Whale SELLS the opposite -> Opposite but no fire (phase 1).
	if _, kind, fire := c.Match("tokB", "sell", "0xwhale"); kind != Opposite || fire {
		t.Errorf("opposite sell: kind=%v fire=%v, want Opposite/false", kind, fire)
	}
}

func TestCache_SelfGuardAndUnheld(t *testing.T) {
	c := NewCache("0xME") // case-insensitive
	c.Replace(held())
	if _, _, fire := c.Match("tokA", "sell", "0xme"); fire {
		t.Error("own wallet must never match")
	}
	if _, kind, _ := c.Match("other", "sell", "0xwhale"); kind != None {
		t.Errorf("unheld token: kind=%v, want None", kind)
	}
	if _, _, fire := c.Match("dust", "sell", "0xwhale"); fire {
		t.Error("dust position should have been dropped")
	}
}

func TestCache_NotLoaded(t *testing.T) {
	c := NewCache("0xme")
	if c.Loaded() {
		t.Error("Loaded should be false before Replace")
	}
	if _, _, fire := c.Match("tokA", "sell", "0xwhale"); fire {
		t.Error("no match before a snapshot is loaded")
	}
}
