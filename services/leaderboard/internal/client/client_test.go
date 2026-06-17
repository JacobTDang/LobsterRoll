package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseLeaderboard(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "profit_7d.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	entries, err := ParseLeaderboard(data)
	if err != nil {
		t.Fatalf("ParseLeaderboard: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("len = %d, want 5", len(entries))
	}
	// API returns entries sorted by amount descending; preserve that order.
	if got := entries[0].Wallet; got != "0x96cfcb0c30942cfcd1cdf76c7d408794d66b1acb" {
		t.Errorf("entries[0].Wallet = %q", got)
	}
	if got := entries[0].Amount; got != 9238344.623225637 {
		t.Errorf("entries[0].Amount = %v", got)
	}
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Amount < entries[i].Amount {
			t.Errorf("entries not sorted desc at %d: %v < %v", i, entries[i-1].Amount, entries[i].Amount)
		}
	}
}

func TestParseLeaderboard_Errors(t *testing.T) {
	if _, err := ParseLeaderboard([]byte("not json")); err == nil {
		t.Error("expected error for malformed json")
	}
	entries, err := ParseLeaderboard([]byte("[]"))
	if err != nil {
		t.Fatalf("empty array: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("empty array -> %d entries", len(entries))
	}
}

func TestFetch(t *testing.T) {
	// body includes a checksummed dup of the second wallet to prove
	// normalization + de-duplication.
	const body = `[
		{"proxyWallet":"0xF0318C32136C2DB7FEC88B84869AEE6A1106C80C","amount":100.0},
		{"proxyWallet":"0x26437896ed9dfeb2f69765edcafe8fdceaab39ae","amount":90.0},
		{"proxyWallet":"0xf0318c32136c2db7fec88b84869aee6a1106c80c","amount":80.0},
		{"proxyWallet":"0xe549581668a5751c1972d3ad2d1991d900bd2d54","amount":70.0}
	]`

	tests := []struct {
		name       string
		metric     Metric
		window     Window
		topN       int
		wantPath   string
		wantWindow string
		wantLimit  string
		want       []string
	}{
		{
			name: "pnl maps to /profit", metric: MetricPNL, window: "7d", topN: 3,
			wantPath: "/profit", wantWindow: "7d", wantLimit: "3",
			want: []string{
				"0xf0318c32136c2db7fec88b84869aee6a1106c80c",
				"0x26437896ed9dfeb2f69765edcafe8fdceaab39ae",
				"0xe549581668a5751c1972d3ad2d1991d900bd2d54",
			},
		},
		{
			name: "volume maps to /volume", metric: MetricVolume, window: "30d", topN: 10,
			wantPath: "/volume", wantWindow: "30d", wantLimit: "10",
			want: []string{
				"0xf0318c32136c2db7fec88b84869aee6a1106c80c",
				"0x26437896ed9dfeb2f69765edcafe8fdceaab39ae",
				"0xe549581668a5751c1972d3ad2d1991d900bd2d54",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.wantPath {
					t.Errorf("path = %q, want %q", r.URL.Path, tt.wantPath)
				}
				if g := r.URL.Query().Get("window"); g != tt.wantWindow {
					t.Errorf("window = %q, want %q", g, tt.wantWindow)
				}
				if g := r.URL.Query().Get("limit"); g != tt.wantLimit {
					t.Errorf("limit = %q, want %q", g, tt.wantLimit)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()

			c := New(srv.URL, srv.Client())
			got, err := c.Fetch(context.Background(), tt.metric, tt.window, tt.topN)
			if err != nil {
				t.Fatalf("Fetch: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Fetch wallets = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFetch_TruncatesToTopN(t *testing.T) {
	const body = `[
		{"proxyWallet":"0xf0318c32136c2db7fec88b84869aee6a1106c80c","amount":100.0},
		{"proxyWallet":"0x26437896ed9dfeb2f69765edcafe8fdceaab39ae","amount":90.0},
		{"proxyWallet":"0xe549581668a5751c1972d3ad2d1991d900bd2d54","amount":80.0}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	got, err := New(srv.URL, srv.Client()).Fetch(context.Background(), MetricPNL, "7d", 2)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (truncated to topN)", len(got))
	}
}

func TestFetch_Validation(t *testing.T) {
	c := New("http://unused.invalid", http.DefaultClient)
	if _, err := c.Fetch(context.Background(), MetricPNL, "7d", 0); err == nil {
		t.Error("topN=0 should error (API treats limit=0 as unlimited)")
	}
	if _, err := c.Fetch(context.Background(), MetricPNL, "weekly", 5); err == nil {
		t.Error("invalid window should error")
	}
	if _, err := c.Fetch(context.Background(), Metric("bogus"), "7d", 5); err == nil {
		t.Error("invalid metric should error")
	}
}

func TestFetch_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid request"}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL, srv.Client()).Fetch(context.Background(), MetricPNL, "7d", 5)
	if err == nil {
		t.Fatal("expected error on HTTP 400")
	}
}
