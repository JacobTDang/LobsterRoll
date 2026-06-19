package book

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestBook_FetchAndDerive(t *testing.T) {
	// Deliberately unsorted to prove we sort: best bid 0.48, best ask 0.52.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token_id") != "tok" {
			t.Errorf("token_id = %q", r.URL.Query().Get("token_id"))
		}
		_, _ = w.Write([]byte(`{
			"bids":[{"price":"0.45","size":"100"},{"price":"0.48","size":"200"}],
			"asks":[{"price":"0.55","size":"100"},{"price":"0.52","size":"300"}]
		}`))
	}))
	defer srv.Close()

	b, err := New(srv.URL, srv.Client()).Book(context.Background(), "tok")
	if err != nil {
		t.Fatalf("Book: %v", err)
	}
	if b.Bids[0].Price != 0.48 || b.Asks[0].Price != 0.52 {
		t.Fatalf("best bid/ask = %v/%v, want 0.48/0.52", b.Bids[0].Price, b.Asks[0].Price)
	}
	if mid, ok := b.Mid(); !ok || !approx(mid, 0.50) {
		t.Errorf("mid = %v, want 0.50", mid)
	}
	if hs, ok := b.HalfSpread(); !ok || !approx(hs, 0.02) {
		t.Errorf("halfSpread = %v, want 0.02", hs)
	}
}

func TestBook_BuyDepthUSD(t *testing.T) {
	b := Book{Asks: []Level{
		{Price: 0.52, Size: 300}, // 156 USD, within band
		{Price: 0.53, Size: 100}, // 53 USD, within band (limit 0.54)
		{Price: 0.60, Size: 999}, // outside band -> excluded
	}}
	// band 0.02 -> limit 0.54 -> levels at 0.52 and 0.53 -> 0.52*300 + 0.53*100 = 209.
	if got := b.BuyDepthUSD(0.02); !approx(got, 209) {
		t.Errorf("BuyDepthUSD = %v, want 209", got)
	}
	if got := (Book{}).BuyDepthUSD(0.02); got != 0 {
		t.Errorf("empty book depth = %v, want 0", got)
	}
}

func TestBook_EmptySideNoMid(t *testing.T) {
	if _, ok := (Book{Bids: []Level{{Price: 0.4, Size: 1}}}).Mid(); ok {
		t.Error("Mid should be false with no asks")
	}
	if _, ok := (Book{}).HalfSpread(); ok {
		t.Error("HalfSpread should be false on empty book")
	}
}

func TestBook_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"bids":[{"price":"x","size":"1"}],"asks":[]}`))
	}))
	defer srv.Close()
	if _, err := New(srv.URL, srv.Client()).Book(context.Background(), "tok"); err == nil {
		t.Error("expected parse error on non-numeric price")
	}
}
