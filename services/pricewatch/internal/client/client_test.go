package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMidpoint_ParsesStringPrice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("token_id"); got != "tok123" {
			t.Errorf("token_id = %q, want tok123", got)
		}
		_, _ = w.Write([]byte(`{"mid":"0.523"}`))
	}))
	defer srv.Close()

	mid, err := New(srv.URL, srv.Client()).Midpoint(context.Background(), "tok123")
	if err != nil {
		t.Fatalf("Midpoint: %v", err)
	}
	if mid != 0.523 {
		t.Errorf("mid = %v, want 0.523", mid)
	}
}

func TestMidpoint_Errors(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"bad json":   func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`not json`)) },
		"bad mid":    func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"mid":"NaNxyz"}`)) },
		"server 500": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) },
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			if _, err := New(srv.URL, srv.Client()).Midpoint(context.Background(), "t"); err == nil {
				t.Errorf("%s: expected error", name)
			}
		})
	}
}
