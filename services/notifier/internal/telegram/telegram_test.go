package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
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
