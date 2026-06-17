package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const (
	tokUnder = "25960997961246252830800085989836468476752301777787246680725159102517868182787" // index 1
	tokOver  = "73821310775952269249751819555967133690885921314127147124070076047215360806437" // index 0
)

func goldenBody(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "gamma_market.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

func TestResolve_Golden(t *testing.T) {
	body := goldenBody(t)

	// Token at index 1 -> "Under".
	e, ok, err := Resolve(body, tokUnder)
	if err != nil || !ok {
		t.Fatalf("Resolve(under): ok=%v err=%v", ok, err)
	}
	if e.MarketQuestion != "Ghana vs. Panama: O/U 2.5" {
		t.Errorf("question = %q", e.MarketQuestion)
	}
	if e.Outcome != "Under" {
		t.Errorf("outcome = %q, want Under", e.Outcome)
	}
	if e.MarketSlug != "fifwc-gha-pan-2026-06-17-total-2pt5" {
		t.Errorf("slug = %q", e.MarketSlug)
	}
	if e.ConditionID != "0x10b86541c2718fecfb2f49d65fd9109de8d05ac3ca7177edf304e317d235e1f6" {
		t.Errorf("conditionId = %q", e.ConditionID)
	}

	// Token at index 0 -> "Over".
	e2, ok, err := Resolve(body, tokOver)
	if err != nil || !ok {
		t.Fatalf("Resolve(over): ok=%v err=%v", ok, err)
	}
	if e2.Outcome != "Over" {
		t.Errorf("outcome = %q, want Over", e2.Outcome)
	}
}

func TestResolve_NotFound(t *testing.T) {
	if _, ok, err := Resolve([]byte("[]"), tokUnder); err != nil || ok {
		t.Errorf("empty array: ok=%v err=%v, want not-found", ok, err)
	}
	// A token not present in the returned market.
	if _, ok, _ := Resolve(goldenBody(t), "999"); ok {
		t.Error("unknown token should be not-found")
	}
}

func TestResolve_BadJSON(t *testing.T) {
	if _, _, err := Resolve([]byte("not json"), tokUnder); err == nil {
		t.Error("expected error on malformed json")
	}
}

func TestFetch(t *testing.T) {
	var gotPath, gotQuery, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("clob_token_ids")
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(goldenBody(t))
	}))
	defer srv.Close()

	e, ok, err := New(srv.URL, srv.Client()).Fetch(context.Background(), tokUnder)
	if err != nil || !ok {
		t.Fatalf("Fetch: ok=%v err=%v", ok, err)
	}
	if gotPath != "/markets" {
		t.Errorf("path = %q, want /markets", gotPath)
	}
	if gotQuery != tokUnder {
		t.Errorf("clob_token_ids = %q, want %q", gotQuery, tokUnder)
	}
	if gotUA != userAgent {
		t.Errorf("UA = %q, want %q", gotUA, userAgent)
	}
	if e.Outcome != "Under" {
		t.Errorf("outcome = %q", e.Outcome)
	}
}

func TestFetch_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()
	if _, ok, err := New(srv.URL, srv.Client()).Fetch(context.Background(), "404tok"); err != nil || ok {
		t.Errorf("ok=%v err=%v, want not-found, no error", ok, err)
	}
}

func TestFetch_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	if _, _, err := New(srv.URL, srv.Client()).Fetch(context.Background(), tokUnder); err == nil {
		t.Error("expected error on HTTP 500")
	}
}
