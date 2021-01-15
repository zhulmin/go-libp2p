package main

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/peer"
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
	time.Sleep(10 * time.Second)
	fmt.Println("\n client ID is ", h1.ID().Pretty())
	fmt.Println("\n client Addrs are", h1.Addrs())

	// connect
	id, err := peer.Decode("QmeCMhrXDSvNyLtfeoGL1wxET2zzgLhfR3SZTihAerK2Eo")
	if err != nil {
		panic(err)
	}

	addrs1 := ma.StringCast("/ip4/147.75.70.221/tcp/4001/p2p/Qme8g49gm3q4Acp7xWBKg3nAa9fxZ1YmyDJdyGgoG6LsXh/p2p-circuit")
	addrs2 := ma.StringCast("/ip4/147.75.195.153/udp/4001/quic/p2p/QmW9m57aiBDHAkKj9nmFSEn7ZqrcF1fZS4bipsTCHburei/p2p-circuit")
	info := peer.AddrInfo{
		ID:    id,
		Addrs: []ma.Multiaddr{addrs1, addrs2},
	}
	if err := h1.Connect(ctx, info); err != nil {
		panic(err)
	}

	select {
	case <-time.After(1 * time.Hour):
	}

}
