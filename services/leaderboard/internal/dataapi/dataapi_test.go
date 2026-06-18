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

// maxRows caps both the request loop and the returned slice.
func TestActivity_RespectsMaxRows(t *testing.T) {
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

	// maxRows 750 -> needs 2 pages (1000 fetched) then truncated to 750.
	acts, err := New(srv.URL, srv.Client()).Activity(context.Background(), "0xabc", 750)
	if err != nil {
		t.Fatalf("Activity: %v", err)
	}
	if len(acts) != 750 {
		t.Fatalf("len = %d, want 750 (truncated to maxRows)", len(acts))
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

func TestTraded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/traded" {
			t.Errorf("path = %q, want /traded", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"user":"0xabc","traded":29}`))
	}))
	defer srv.Close()
	n, err := New(srv.URL, srv.Client()).Traded(context.Background(), "0xabc")
	if err != nil {
		t.Fatalf("Traded: %v", err)
	}
	if n != 29 {
		t.Errorf("Traded = %d, want 29", n)
	}
}

func TestSetsUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"user":"0xabc","traded":1}`))
	}))
	defer srv.Close()
	if _, err := New(srv.URL, srv.Client()).Traded(context.Background(), "0xabc"); err != nil {
		t.Fatalf("Traded: %v", err)
	}
	if gotUA != userAgent {
		t.Errorf("User-Agent = %q, want %q", gotUA, userAgent)
	}
}

func TestRetriesTransient(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&hits, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"user":"0xabc","traded":7}`))
	}))
	defer srv.Close()
	n, err := New(srv.URL, srv.Client()).Traded(context.Background(), "0xabc")
	if err != nil {
		t.Fatalf("Traded: %v", err)
	}
	if n != 7 {
		t.Errorf("Traded = %d, want 7", n)
	}
	if h := atomic.LoadInt32(&hits); h != 3 {
		t.Errorf("hits = %d, want 3", h)
	}
}

func TestNoRetryOn4xx(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	if _, err := New(srv.URL, srv.Client()).Traded(context.Background(), "0xabc"); err == nil {
		t.Fatal("expected error on 400")
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Errorf("hits = %d, want 1 (no retry on 4xx)", h)
	}
}
