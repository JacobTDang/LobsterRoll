package dataapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestGetJSON(t *testing.T) {
	var gotPath, gotQuery, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotUA = r.URL.Path, r.URL.RawQuery, r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`[{"user":"0xa","value":12.5}]`))
	}))
	defer srv.Close()

	c := New(srv.URL, "test-ua/1.0", srv.Client())
	q := url.Values{}
	q.Set("user", "0xa")
	var out []struct {
		User  string  `json:"user"`
		Value float64 `json:"value"`
	}
	if err := c.GetJSON(context.Background(), "/value", q, &out); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if gotPath != "/value" || gotQuery != "user=0xa" {
		t.Errorf("request = %s?%s, want /value?user=0xa", gotPath, gotQuery)
	}
	if gotUA != "test-ua/1.0" {
		t.Errorf("user-agent = %q, want test-ua/1.0", gotUA)
	}
	if len(out) != 1 || out[0].Value != 12.5 {
		t.Fatalf("decoded = %+v", out)
	}
}

func TestNew_Defaults(t *testing.T) {
	if c := New("", "ua", nil); c.baseURL != BaseURL || c.http == nil {
		t.Fatalf("defaults not applied: baseURL=%q http=%v", c.baseURL, c.http)
	}
}
