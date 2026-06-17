package marketdata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const (
	tokOver  = "73821310775952269249751819555967133690885921314127147124070076047215360806437" // idx 0, price 0.415
	tokUnder = "25960997961246252830800085989836468476752301777787246680725159102517868182787" // idx 1, price 0.585
)

func golden(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "gamma_market.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

func TestParse_Golden(t *testing.T) {
	d, ok, err := Parse(golden(t), tokUnder)
	if err != nil || !ok {
		t.Fatalf("Parse(under): ok=%v err=%v", ok, err)
	}
	if d.CurrentPrice != 0.585 {
		t.Errorf("price = %v, want 0.585", d.CurrentPrice)
	}
	if d.LiquidityUSD != 688478.9052 {
		t.Errorf("liquidity = %v, want 688478.9052", d.LiquidityUSD)
	}
	if d.ConditionID != "0x10b86541c2718fecfb2f49d65fd9109de8d05ac3ca7177edf304e317d235e1f6" {
		t.Errorf("conditionId = %q", d.ConditionID)
	}
	if !d.Active {
		t.Errorf("Active = false, want true (active && !closed)")
	}

	d0, _, _ := Parse(golden(t), tokOver)
	if d0.CurrentPrice != 0.415 {
		t.Errorf("over price = %v, want 0.415", d0.CurrentPrice)
	}
}

func TestParse_NotFound(t *testing.T) {
	if _, ok, err := Parse([]byte("[]"), tokUnder); err != nil || ok {
		t.Errorf("empty: ok=%v err=%v, want not-found", ok, err)
	}
	if _, ok, _ := Parse(golden(t), "999"); ok {
		t.Error("unknown token should be not-found")
	}
}

func TestFetch(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("clob_token_ids")
		_, _ = w.Write(golden(t))
	}))
	defer srv.Close()

	d, ok, err := New(srv.URL, srv.Client()).Fetch(context.Background(), tokUnder)
	if err != nil || !ok {
		t.Fatalf("Fetch: ok=%v err=%v", ok, err)
	}
	if gotQuery != tokUnder {
		t.Errorf("query = %q", gotQuery)
	}
	if d.CurrentPrice != 0.585 {
		t.Errorf("price = %v", d.CurrentPrice)
	}
}
