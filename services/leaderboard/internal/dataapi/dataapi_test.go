package dataapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
)

func TestActivity_SinglePage(t *testing.T) {
	const body = `[
		{"type":"TRADE","side":"BUY","usdcSize":100.5,"conditionId":"0xmkt1"},
		{"type":"REDEEM","side":"","usdcSize":150.0,"conditionId":"0xmkt1"}
	]`
	var gotUser, gotLimit, gotOffset string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/activity" {
			t.Errorf("path = %q, want /activity", r.URL.Path)
		}
		gotUser = r.URL.Query().Get("user")
		gotLimit = r.URL.Query().Get("limit")
		gotOffset = r.URL.Query().Get("offset")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	acts, err := New(srv.URL, srv.Client()).Activity(context.Background(), "0xabc", 1000)
	if err != nil {
		t.Fatalf("Activity: %v", err)
	}
	if len(acts) != 2 {
		t.Fatalf("len = %d, want 2", len(acts))
	}
	if acts[0].Type != "TRADE" || acts[0].Side != "BUY" || acts[0].USDCSize != 100.5 || acts[0].ConditionID != "0xmkt1" {
		t.Errorf("acts[0] = %+v", acts[0])
	}
	if gotUser != "0xabc" || gotLimit != "500" || gotOffset != "0" {
		t.Errorf("query user=%q limit=%q offset=%q", gotUser, gotLimit, gotOffset)
	}
}

// Multi-page: the server returns full pages until a short page; pagination must
// advance offset by 500 each call and stop on the short page.
func TestActivity_Paginates(t *testing.T) {
	// 2 full pages (500 each) then a short page of 3 -> 1003 total rows.
	pages := map[int]int{0: 500, 500: 500, 1000: 3}
	var offsets []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		off, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		offsets = append(offsets, off)
		n := pages[off]
		_, _ = w.Write([]byte("["))
		for i := 0; i < n; i++ {
			if i > 0 {
				_, _ = w.Write([]byte(","))
			}
			fmt.Fprintf(w, `{"type":"TRADE","side":"BUY","usdcSize":1,"conditionId":"m%d"}`, off+i)
		}
		_, _ = w.Write([]byte("]"))
	}))
	defer srv.Close()

	acts, err := New(srv.URL, srv.Client()).Activity(context.Background(), "0xabc", 100000)
	if err != nil {
		t.Fatalf("Activity: %v", err)
	}
	if len(acts) != 1003 {
		t.Fatalf("len = %d, want 1003", len(acts))
	}
	want := []int{0, 500, 1000}
	if len(offsets) != len(want) {
		t.Fatalf("offsets = %v, want %v", offsets, want)
	}
	for i := range want {
		if offsets[i] != want[i] {
			t.Fatalf("offsets = %v, want %v", offsets, want)
		}
	}
}

// maxRows is a page-boundary SAFETY CEILING, not an exact slice: it must never
// cut mid-page (which would corrupt a market's cost basis in stats.Compute).
func TestActivity_StopsAtCeilingOnPageBoundary(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte("["))
		for i := 0; i < 500; i++ {
			if i > 0 {
				_, _ = w.Write([]byte(","))
			}
			_, _ = w.Write([]byte(`{"type":"TRADE","side":"BUY","usdcSize":1,"conditionId":"m"}`))
		}
		_, _ = w.Write([]byte("]"))
	}))
	defer srv.Close()

	// maxRows 750 with 500/page: fetches 2 full pages (1000) then stops at the page
	// boundary — returns 1000, NOT a mid-page slice of 750.
	acts, err := New(srv.URL, srv.Client()).Activity(context.Background(), "0xabc", 750)
	if err != nil {
		t.Fatalf("Activity: %v", err)
	}
	if len(acts) != 1000 {
		t.Fatalf("len = %d, want 1000 (page boundary, never sliced mid-page)", len(acts))
	}
	if c := atomic.LoadInt32(&calls); c != 2 {
		t.Errorf("page fetches = %d, want 2", c)
	}
}

func TestActivity_ZeroMaxRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server should not be hit for maxRows<=0")
	}))
	defer srv.Close()
	acts, err := New(srv.URL, srv.Client()).Activity(context.Background(), "0xabc", 0)
	if err != nil {
		t.Fatalf("Activity: %v", err)
	}
	if len(acts) != 0 {
		t.Errorf("len = %d, want 0", len(acts))
	}
}

func TestValue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/value" {
			t.Errorf("path = %q, want /value", r.URL.Path)
		}
		if u := r.URL.Query().Get("user"); u != "0xabc" {
			t.Errorf("user = %q", u)
		}
		_, _ = w.Write([]byte(`[{"user":"0xabc","value":12345.67}]`))
	}))
	defer srv.Close()

	v, err := New(srv.URL, srv.Client()).Value(context.Background(), "0xabc")
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if v != 12345.67 {
		t.Errorf("Value = %v, want 12345.67", v)
	}
}

func TestValue_NoMatchingWalletReturnsZero(t *testing.T) {
	// A present-but-mismatched row must NOT be returned (it would bypass the
	// no-portfolio fallback and feed a wrong figure to the portfolio gate).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"user":"0xSOMEONEELSE","value":999999}]`))
	}))
	defer srv.Close()
	v, err := New(srv.URL, srv.Client()).Value(context.Background(), "0xabc")
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if v != 0 {
		t.Errorf("Value = %v, want 0 (no row matches queried wallet)", v)
	}
}

func TestValue_EmptyArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	v, err := New(srv.URL, srv.Client()).Value(context.Background(), "0xabc")
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if v != 0 {
		t.Errorf("Value = %v, want 0 for empty portfolio", v)
	}
}
