package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchEntries(t *testing.T) {
	// includes a checksummed dup of the first wallet (must de-dup, first wins).
	const body = `[
		{"proxyWallet":"0xF0318C32136C2DB7FEC88B84869AEE6A1106C80C","amount":100.0},
		{"proxyWallet":"0x26437896ed9dfeb2f69765edcafe8fdceaab39ae","amount":90.0},
		{"proxyWallet":"0xf0318c32136c2db7fec88b84869aee6a1106c80c","amount":80.0}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/profit" {
			t.Errorf("path = %q, want /profit", r.URL.Path)
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	got, err := New(srv.URL, srv.Client()).FetchEntries(context.Background(), MetricPNL, "30d", 10)
	if err != nil {
		t.Fatalf("FetchEntries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (deduped)", len(got))
	}
	if got[0].Wallet != "0xf0318c32136c2db7fec88b84869aee6a1106c80c" || got[0].Amount != 100.0 {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Wallet != "0x26437896ed9dfeb2f69765edcafe8fdceaab39ae" || got[1].Amount != 90.0 {
		t.Errorf("got[1] = %+v", got[1])
	}
}

func TestFetchEntries_TruncatesToTopN(t *testing.T) {
	const body = `[
		{"proxyWallet":"0xf0318c32136c2db7fec88b84869aee6a1106c80c","amount":100.0},
		{"proxyWallet":"0x26437896ed9dfeb2f69765edcafe8fdceaab39ae","amount":90.0},
		{"proxyWallet":"0xe549581668a5751c1972d3ad2d1991d900bd2d54","amount":80.0}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	got, err := New(srv.URL, srv.Client()).FetchEntries(context.Background(), MetricPNL, "30d", 2)
	if err != nil {
		t.Fatalf("FetchEntries: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

func TestFetchEntries_EmptyErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()
	if _, err := New(srv.URL, srv.Client()).FetchEntries(context.Background(), MetricPNL, "30d", 5); err == nil {
		t.Fatal("expected error for empty leaderboard")
	}
}

func TestFetchEntries_Validation(t *testing.T) {
	c := New("http://unused.invalid", http.DefaultClient)
	if _, err := c.FetchEntries(context.Background(), MetricPNL, "30d", 0); err == nil {
		t.Error("topN=0 should error")
	}
	if _, err := c.FetchEntries(context.Background(), MetricPNL, "weekly", 5); err == nil {
		t.Error("invalid window should error")
	}
	if _, err := c.FetchEntries(context.Background(), Metric("bogus"), "30d", 5); err == nil {
		t.Error("invalid metric should error")
	}
}
