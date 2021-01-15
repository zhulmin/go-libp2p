package main

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p"
	quic "github.com/libp2p/go-libp2p-quic-transport"
	"github.com/libp2p/go-tcp-transport"
	ma "github.com/multiformats/go-multiaddr"
)

func main() {
	ctx := context.Background()
	h1, err := libp2p.New(ctx, libp2p.DefaultStaticRelays(), libp2p.ForceReachabilityPrivate(), libp2p.EnableAutoRelay(),
		libp2p.Transport(quic.NewTransport), libp2p.Transport(tcp.NewTCPTransport), libp2p.ListenAddrs(ma.StringCast("/ip4/0.0.0.0/udp/12345/quic"),
			ma.StringCast("/ip4/0.0.0.0/tcp/12345")))
	if err != nil {
		panic(err)
	}
	time.Sleep(20 * time.Second)
	fmt.Println("\n h1 ID is ", h1.ID().Pretty())
	fmt.Println("\n h1 Addrs are", h1.Addrs())

	select {
	case <-time.After(1 * time.Hour):
	}
}
