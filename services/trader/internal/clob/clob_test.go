package clob

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Cross-implementation golden: the L2 signature must match an independent
// Python HMAC reference for the same inputs.
func TestL2Sign_GoldenVector(t *testing.T) {
	secret := "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	got, err := l2Sign(secret, "1700000000", "POST", "/order", `{"x":1}`)
	if err != nil {
		t.Fatalf("l2Sign: %v", err)
	}
	const want = "2t4TPZwjv6fDsCqe3ug1x5NQWmGEc82naGXsZQIdsWE=" // python hmac reference
	if got != want {
		t.Fatalf("l2Sign = %q, want %q", got, want)
	}
}

func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(srv.URL, Creds{APIKey: "k", Secret: "MDEyMzQ1Njc4OWFiY2RlZg==", Passphrase: "p", Address: "0xabc"}, srv.Client())
	c.now = func() time.Time { return time.Unix(1700000000, 0) }
	return c, srv
}

var sample = SignedOrder{Salt: "1", Maker: "0xm", Signer: "0xs", TokenID: "123", MakerAmount: "25000000", TakerAmount: "50000000", Side: "BUY", SignatureType: 0, Timestamp: "1700000000000", Signature: "0xsig"}

func TestPlaceOrder_AcceptedSendsSignedPayloadAndHeaders(t *testing.T) {
	var gotHeaders http.Header
	var gotBody placeReq
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		_, _ = w.Write([]byte(`{"success":true,"orderID":"ord-1","status":"matched"}`))
	})

	res, err := c.PlaceOrder(context.Background(), sample)
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	if !res.Success || res.OrderID != "ord-1" || res.Status != "matched" {
		t.Fatalf("result = %+v", res)
	}
	// L2 headers present.
	for _, h := range []string{"Poly_Address", "Poly_Api_Key", "Poly_Passphrase", "Poly_Timestamp", "Poly_Signature"} {
		if gotHeaders.Get(h) == "" {
			t.Errorf("missing header %s", h)
		}
	}
	// Signed order body carried through.
	if gotBody.Order.Signature != "0xsig" || gotBody.Order.Side != "BUY" || gotBody.OrderType != "GTC" {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestPlaceOrder_Rejected(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":false,"orderID":"ord-2","status":"unmatched"}`))
	})
	if _, err := c.PlaceOrder(context.Background(), sample); err == nil {
		t.Fatal("expected error on success:false")
	}
}

func TestPlaceOrder_HTTPError(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad order"}`))
	})
	if _, err := c.PlaceOrder(context.Background(), sample); err == nil {
		t.Fatal("expected error on HTTP 400")
	}
}

func TestPlaceOrder_Partial(t *testing.T) {
	// A partial fill is still a success; status reflects it.
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"orderID":"ord-3","status":"live"}`))
	})
	res, err := c.PlaceOrder(context.Background(), sample)
	if err != nil || res.Status != "live" {
		t.Fatalf("partial: res=%+v err=%v", res, err)
	}
}
