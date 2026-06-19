package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSend_PayloadAndPath(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody sendMessageReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "TESTTOKEN", srv.Client())
	if err := c.Send(context.Background(), "12345", "hello whale"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/botTESTTOKEN/sendMessage" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody.ChatID != "12345" || gotBody.Text != "hello whale" {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestSend_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	}))
	defer srv.Close()
	err := New(srv.URL, "T", srv.Client()).Send(context.Background(), "1", "x")
	if err == nil {
		t.Fatal("expected error on ok:false")
	}
}

func TestSend_RetriesOn429(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"description":"Too Many Requests","parameters":{"retry_after":0}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	start := time.Now()
	if err := New(srv.URL, "T", srv.Client()).Send(context.Background(), "1", "x"); err != nil {
		t.Fatalf("Send should succeed after a 429 retry: %v", err)
	}
	if h := atomic.LoadInt32(&hits); h != 2 {
		t.Errorf("hits = %d, want 2 (429 then 200)", h)
	}
	// retry_after was 0, so the >=1s floor must apply (no instant-retry busy-loop).
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Errorf("retry waited %v, want >=~1s (429 backoff floor)", elapsed)
	}
}

func TestSend_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false,"description":"Unauthorized"}`))
	}))
	defer srv.Close()
	if err := New(srv.URL, "T", srv.Client()).Send(context.Background(), "1", "x"); err == nil {
		t.Fatal("expected error on HTTP 401")
	}
}
