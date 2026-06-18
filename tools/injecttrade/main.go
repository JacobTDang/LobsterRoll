// Command injecttrade publishes a synthetic trades.detected event so you can
// exercise the enrich → alert → approve pipeline without waiting for a real
// whale trade (or even an RPC connection).
//
//	go run ./tools/injecttrade -side buy -size 5.76 -price 0.95
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
)

func main() {
	natsURL := flag.String("nats", "nats://localhost:4222", "NATS URL")
	wallet := flag.String("wallet", "0x037c0f46600702e77ccb738721a78d6418d3a458", "whale wallet (used when -n=1)")
	token := flag.String("token", "25960997961246252830800085989836468476752301777787246680725159102517868182787", "tokenId")
	live := flag.Bool("live", false, "fetch a current active market token from gamma (overrides -token, shows a real market)")
	side := flag.String("side", "buy", "buy|sell")
	price := flag.String("price", "0.95", "price")
	size := flag.String("size", "5.76", "size (shares)")
	tx := flag.String("tx", "0xdeadbeefcafebabedeadbeefcafebabedeadbeefcafebabedeadbeefcafebabe", "tx hash")
	n := flag.Int("n", 1, "number of trades from DISTINCT wallets on the same token+side (>=3 trips consensus)")
	flag.Parse()

	if *live {
		t, q, err := liveToken()
		if err != nil {
			log.Fatalf("fetch live market: %v", err)
		}
		*token = t
		log.Printf("live market: %q (token %s…)", q, t[:min(8,len(t))])
	}

	pub, err := bus.Connect(*natsURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pub.Close()

	for i := 0; i < *n; i++ {
		w := *wallet
		if *n > 1 {
			w = fmt.Sprintf("0x%040x", i+1) // distinct synthetic wallets to form a cohort
		}
		td := bus.TradeDetected{
			Wallet: w, TokenID: *token, Side: *side, Price: *price, Size: *size,
			TxHash: fmt.Sprintf("%s%02x", (*tx)[:len(*tx)-2], i&0xff), LogIndex: uint64(i), BlockNumber: 1, ObservedAt: time.Now().UTC(),
		}
		if err := pub.PublishTrade(td); err != nil {
			log.Fatalf("publish: %v", err)
		}
		log.Printf("published trades.detected: %s %s shares @ %s by %s (token %s…)", td.Side, td.Size, td.Price, w, td.TokenID[:min(8,len(td.TokenID))])
	}
}

// liveToken fetches the highest-24h-volume active market from gamma and returns
// its first outcome token id + the market question.
func liveToken() (string, string, error) {
	req, _ := http.NewRequest(http.MethodGet,
		"https://gamma-api.polymarket.com/markets?closed=false&active=true&limit=1&order=volume24hr&ascending=false", nil)
	req.Header.Set("User-Agent", "lobsterroll-injecttrade/1.0")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var markets []struct {
		Question      string `json:"question"`
		ClobTokenIDs  string `json:"clobTokenIds"` // JSON-encoded array string
	}
	if err := json.Unmarshal(body, &markets); err != nil {
		return "", "", err
	}
	if len(markets) == 0 {
		return "", "", fmt.Errorf("no active markets returned")
	}
	var toks []string
	if err := json.Unmarshal([]byte(markets[0].ClobTokenIDs), &toks); err != nil || len(toks) == 0 {
		return "", "", fmt.Errorf("market has no clobTokenIds")
	}
	return toks[0], markets[0].Question, nil
}
