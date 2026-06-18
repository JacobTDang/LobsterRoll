// Command injecttrade publishes a synthetic trades.detected event so you can
// exercise the enrich → alert → approve pipeline without waiting for a real
// whale trade (or even an RPC connection).
//
//	go run ./tools/injecttrade -side buy -size 5.76 -price 0.95
package main

import (
	"flag"
	"log"
	"time"

	"github.com/JacobTDang/LobsterRoll/pkg/bus"
)

func main() {
	natsURL := flag.String("nats", "nats://localhost:4222", "NATS URL")
	wallet := flag.String("wallet", "0x037c0f46600702e77ccb738721a78d6418d3a458", "whale wallet")
	token := flag.String("token", "25960997961246252830800085989836468476752301777787246680725159102517868182787", "tokenId")
	side := flag.String("side", "buy", "buy|sell")
	price := flag.String("price", "0.95", "price")
	size := flag.String("size", "5.76", "size (shares)")
	tx := flag.String("tx", "0xdeadbeefcafebabedeadbeefcafebabedeadbeefcafebabedeadbeefcafebabe", "tx hash")
	flag.Parse()

	pub, err := bus.Connect(*natsURL)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pub.Close()

	td := bus.TradeDetected{
		Wallet: *wallet, TokenID: *token, Side: *side, Price: *price, Size: *size,
		TxHash: *tx, LogIndex: 1, BlockNumber: 1, ObservedAt: time.Now().UTC(),
	}
	if err := pub.PublishTrade(td); err != nil {
		log.Fatalf("publish: %v", err)
	}
	log.Printf("published trades.detected: %s %s %s @ %s (token %s…)", td.Side, td.Size, "shares", td.Price, td.TokenID[:8])
}
