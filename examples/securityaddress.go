package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"

	ma "github.com/multiformats/go-multiaddr"
)

func main() {
	fmt.Println("hallo")
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	h1, err := newHost(1234)
	if err != nil {
		return err
	}
	defer h1.Close()
	log.Printf("Host 1 (%s) listening at %v", h1.ID(), h1.Addrs())
	h2, err := newHost(1235)
	if err != nil {
		return err
	}
	defer h2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h1.Connect(ctx, peer.AddrInfo{ID: h2.ID(), Addrs: h2.Addrs()}); err != nil {
		return err
	}
	fmt.Println("h1 connected to h2")
	return nil
}

func newHost(port int) (host.Host, error) {
	tls, err := ma.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/tls", port))
	if err != nil {
		return nil, err
	}
	return libp2p.New(
		context.Background(),
		libp2p.ListenAddrs(tls),
	)
}
