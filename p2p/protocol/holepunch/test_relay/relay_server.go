package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p"
	circuit "github.com/libp2p/go-libp2p-circuit"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/event"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	quic "github.com/libp2p/go-libp2p-quic-transport"
	"github.com/libp2p/go-tcp-transport"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

var (
	private_key_hex = "08011240004524a4b28af09bbcf05cec2d0317796ef5c7ce05e60df2217cb9a885791f86e35db6f2309e8decee7e6d72da1bb627e9221bb87bdc436761e6f9e6024fc568"
)

func main() {
	skbz, err := hex.DecodeString(private_key_hex)
	if err != nil {
		panic(err)
	}
	sk, err := crypto.UnmarshalPrivateKey(skbz)
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	h1, err := libp2p.New(ctx,
		libp2p.Identity(sk),
		libp2p.EnableRelay(circuit.OptHop),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Transport(quic.NewTransport),
		libp2p.ListenAddrs(
			ma.StringCast("/ip4/0.0.0.0/tcp/12345"),
			ma.StringCast("/ip4/0.0.0.0/udp/12345/quic"),
		))

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
			panic(errors.New("did not discover address for self"))
		}
	}
	fmt.Println("relay server peerID is", h1.ID().Pretty())
	fmt.Println("relay server addresses are:")
	for _, a := range h1.Addrs() {
		fmt.Println(a)
	}

	//  no wait for conns
	for {

	}
}
