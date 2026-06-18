// Command natsd runs a standalone in-process NATS server for local development
// (no Docker required). Default address: nats://0.0.0.0:4222.
//
//	go run ./tools/natsd            # :4222
//	go run ./tools/natsd -port 4333
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats-server/v2/server"
)

func main() {
	port := flag.Int("port", 4222, "client port")
	host := flag.String("host", "127.0.0.1", "bind host (loopback by default; this is an unauthenticated dev broker)")
	flag.Parse()

	s, err := server.NewServer(&server.Options{Host: *host, Port: *port})
	if err != nil {
		log.Fatalf("natsd: %v", err)
	}
	go s.Start()
	if !s.ReadyForConnections(10 * time.Second) {
		log.Fatal("natsd: not ready for connections")
	}
	log.Printf("natsd listening on %s", s.ClientURL())

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Print("natsd shutting down")
	s.Shutdown()
	s.WaitForShutdown()
}
