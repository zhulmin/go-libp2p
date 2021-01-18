package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/event"
	"github.com/libp2p/go-libp2p-core/peer"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	quic "github.com/libp2p/go-libp2p-quic-transport"
	"github.com/libp2p/go-tcp-transport"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

var (
	relay_server_ID      = "12D3KooWR7ubdas2nrgK3Y2mE9A27i5WubjhkzgrMKkEeEvzB6Cw"
	relay_server_address = []ma.Multiaddr{ma.StringCast("/ip4/54.255.62.136/udp/12345/quic"),
		ma.StringCast("/ip4/54.255.62.136/tcp/12345")}
)

func main() {
	relay_server, err := peer.Decode(relay_server_ID)
	if err != nil {
		panic(err)
	}

	// create host with hole punching enabled. we also enable AutorRelay with static servers so peer can connect to and
	// advertise relay addresses on it's own.
	ctx := context.Background()
	h1, err := libp2p.New(ctx,
		libp2p.EnableHolePunching(),
		libp2p.EnableAutoRelay(),
		libp2p.ForceReachabilityPrivate(),
		libp2p.StaticRelays([]peer.AddrInfo{
			{ID: relay_server,
				Addrs: relay_server_address},
		}),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Transport(quic.NewTransport),
		libp2p.ListenAddrs(
			ma.StringCast("/ip4/0.0.0.0/tcp/12345"),
			ma.StringCast("/ip4/0.0.0.0/udp/12345/quic"),
		),
	)
	if err != nil {
		panic(err)
	}
	// subscribe for address change event so we can detect when we discover an observed public non proxy address
	sub, err := h1.EventBus().Subscribe(new(event.EvtLocalAddressesUpdated))
	if err != nil {
		panic(err)
	}

	// bootstrap with dht so we have some connections and activated observed addresses and our address gets advertised to the world.
	d, err := dht.New(ctx, h1, dht.Mode(dht.ModeClient), dht.BootstrapPeers(dht.GetDefaultBootstrapPeerAddrInfos()...))
	if err != nil {
		panic(err)
	}
	// block till we have an observed public address.
LOOP:
	for {
		select {
		case ev := <-sub.Out():
			aev := ev.(event.EvtLocalAddressesUpdated)
			for _, a := range aev.Current {
				_, err := a.Address.ValueForProtocol(ma.P_CIRCUIT)
				if manet.IsPublicAddr(a.Address) && err != nil {
					break LOOP
				}
			}
		case <-time.After(300 * time.Second):
			panic(errors.New("did not get public address"))
		}
	}

	fmt.Println("peer ID is", h1.ID().Pretty())
	fmt.Println("peer has discovered it's NATT'd address, known addresses are:")
	for _, a := range h1.Addrs() {
		fmt.Println(a)
	}

	// one more round of refresh so our observed address also gets propagated to the network.
	<-d.ForceRefresh()

	fmt.Println("server peer has advertised addresses to the DHT and is ready for hole punching")
	// accept connections
	for {
	}
}
