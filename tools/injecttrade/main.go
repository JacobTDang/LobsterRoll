// Command injecttrade publishes a synthetic trades.detected event so you can
// exercise the enrich → alert → approve pipeline without waiting for a real
// whale trade (or even an RPC connection).
//
//	go run ./tools/injecttrade -side buy -size 5.76 -price 0.95
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
)

func main() {
	natsURL := flag.String("nats", "nats://localhost:4222", "NATS URL")
	wallet := flag.String("wallet", "0x037c0f46600702e77ccb738721a78d6418d3a458", "whale wallet (used when -n=1)")
	token := flag.String("token", "25960997961246252830800085989836468476752301777787246680725159102517868182787", "tokenId")
	side := flag.String("side", "buy", "buy|sell")
	price := flag.String("price", "0.95", "price")
	size := flag.String("size", "5.76", "size (shares)")
	tx := flag.String("tx", "0xdeadbeefcafebabedeadbeefcafebabedeadbeefcafebabedeadbeefcafebabe", "tx hash")
	n := flag.Int("n", 1, "number of trades from DISTINCT wallets on the same token+side (>=3 trips consensus)")
	flag.Parse()

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
		log.Printf("published trades.detected: %s %s shares @ %s by %s (token %s…)", td.Side, td.Size, td.Price, w, td.TokenID[:8])
	}
}
