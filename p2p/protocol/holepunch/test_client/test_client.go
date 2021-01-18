package main

import (
	"context"
	"errors"
	"flag"
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
	// peer ID of the server
	server_peer_id = "QmW3MoedmkQn1kqsMN9oRoJDfhCxKBHGSyscBGMbzuGcCT"
)

func main() {
	// Configure either TCP hole punching or QUIC hole punching
	var test_mode string
	flag.StringVar(&test_mode, "test_mode", "quic", "Test TCP or QUIC hole punching")
	flag.Parse()
	if test_mode != "tcp" && test_mode != "quic" {
		panic(errors.New("test mode should be tcp or quic"))
	}
	fmt.Println("\n test client initiated in mode:", test_mode)

	// transports and addresses
	var transportOpts []libp2p.Option
	if test_mode == "tcp" {
		transportOpts = append(transportOpts, libp2p.Transport(tcp.NewTCPTransport), libp2p.ListenAddrs(ma.StringCast("/ip4/0.0.0.0/tcp/22345")))
	} else {
		transportOpts = append(transportOpts, libp2p.Transport(quic.NewTransport), libp2p.ListenAddrs(ma.StringCast("/ip4/0.0.0.0/udp/22345/quic")))
	}

	// create host with hole punching enabled.
	ctx := context.Background()
	h1, err := libp2p.New(ctx,
		libp2p.EnableHolePunching(),
		transportOpts[0],
		transportOpts[1])
	if err != nil {
		panic(err)
	}
	// subscribe for address change event so we can detect when we discover an observed public non proxy address
	sub, err := h1.EventBus().Subscribe(new(event.EvtLocalAddressesUpdated))
	if err != nil {
		panic(err)
	}

	// bootstrap with dht so we have some connections and activated observed addresses.
	d, err := dht.New(ctx, h1, dht.Mode(dht.ModeClient), dht.BootstrapPeers(dht.GetDefaultBootstrapPeerAddrInfos()...))
	if err != nil {
		panic(err)
	}
	d.Bootstrap(ctx)

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

	// we should now have some connections and observed addresses
	fmt.Println("client peer ID is", h1.ID().Pretty())
	fmt.Println("client peer has discovered public addresses for self, all addresses are:")
	for _, a := range h1.Addrs() {
		fmt.Println(a)
	}

	// lookup and connect to server peer
	serverPid, err := peer.Decode(server_peer_id)
	if err != nil {
		panic(err)
	}
	pi, err := d.FindPeer(ctx, serverPid)
	if err != nil {
		panic(err)
	}
	fmt.Printf("\n finished looking up server peer on DHT, info is \n %+v", pi)

	// we should have a Relayed connection to the peer as the server peer is:
	// a. NATT'd
	// b. Configured to use AutoRelay.
	err = h1.Connect(ctx, pi)
	if err != nil {
		panic(err)
	}
	time.Sleep(20 * time.Second)
	fmt.Println("connections on client are:")

}
