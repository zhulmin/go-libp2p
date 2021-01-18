package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/peer"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	quic "github.com/libp2p/go-libp2p-quic-transport"
	"github.com/libp2p/go-tcp-transport"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

var (
	// peer ID of the server
	server_peer_id = ""
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
		transportOpts = append(transportOpts, libp2p.Transport(tcp.NewTCPTransport), libp2p.ListenAddrs(ma.StringCast("/ip4/0.0.0.0/tcp/12345")))
	} else {
		transportOpts = append(transportOpts, libp2p.Transport(quic.NewTransport), libp2p.ListenAddrs(ma.StringCast("/ip4/0.0.0.0/udp/12345/quic")))
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

	// bootstrap with dht so we have some connections and activated observed addresses.
	d, err := dht.New(ctx, h1, dht.Mode(dht.ModeClient), dht.BootstrapPeers(dht.GetDefaultBootstrapPeerAddrInfos()...))
	if err != nil {
		panic(err)
	}
	d.Bootstrap(ctx)

	// wait till we have a public non-pxory address i.e. we learn of our observed/NAT-translated addresses by connecting
	// to DHT peers
	time.Sleep(10 * time.Second)
	isPublic := false
	for _, a := range h1.Addrs() {
		_, err := a.ValueForProtocol(ma.P_CIRCUIT)
		if manet.IsPublicAddr(a) && err != nil {
			isPublic = true
		}
	}
	if !isPublic {
		panic(errors.New("do not have a single public address even after 10 seconds"))
	}

	// we should now have some connections and observed addresses
	fmt.Println("\n dht refresh finished, known addresses for client are:")
	for _, a := range h1.Addrs() {
		fmt.Println("\n ", a)
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

	// we should have a Relayed connection to the peer as the server peer is:
	// a. NATT'd
	// b. Configured to use AutoRelay.
	err = h1.Connect(ctx, pi)
	if err != nil {
		panic(err)
	}
	// wait for some time for hole punching to finish and print out all connections.
	fmt.Println("\n")
}
