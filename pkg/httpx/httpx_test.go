package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGet_Success(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	b, err := Get(context.Background(), srv.Client(), srv.URL, "lobster/1.0", 1<<20)
	if err != nil || string(b) != "ok" {
		t.Fatalf("Get = %q, %v", b, err)
	}
	if gotUA != "lobster/1.0" {
		t.Errorf("User-Agent = %q, want lobster/1.0", gotUA)
	}
}

func TestGet_RetriesTransientThenSucceeds(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&hits, 1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable) // 503 -> transient
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	b, err := Get(context.Background(), srv.Client(), srv.URL, "ua", 1<<20)
	if err != nil || string(b) != "recovered" {
		t.Fatalf("Get = %q, %v", b, err)
	}
	if h := atomic.LoadInt32(&hits); h != 3 {
		t.Errorf("hits = %d, want 3 (2 retries)", h)
	}
}

func TestGet_NoRetryOn4xx(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	if _, err := Get(context.Background(), srv.Client(), srv.URL, "ua", 1<<20); err == nil {
		t.Fatal("expected error on 400")
	}
	if h := atomic.LoadInt32(&hits); h != 1 {
		t.Errorf("hits = %d, want 1 (no retry on 4xx)", h)
	}
}

func TestGet_BodyLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 100)))
	}))
	defer srv.Close()

	b, err := Get(context.Background(), srv.Client(), srv.URL, "ua", 10)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(b) != 10 {
		t.Errorf("len = %d, want 10 (bodyLimit)", len(b))
	}
}

func TestGet_GivesUpAfterMaxAttempts(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := Get(context.Background(), srv.Client(), srv.URL, "ua", 1<<20); err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if h := atomic.LoadInt32(&hits); h != maxAttempts {
		t.Errorf("hits = %d, want %d", h, maxAttempts)
	}
}
