package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p"
	circuit "github.com/libp2p/go-libp2p-circuit"
	"github.com/libp2p/go-libp2p-core/crypto"
	ma "github.com/multiformats/go-multiaddr"
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
		libp2p.ListenAddrs(ma.StringCast("/ip4/0.0.0.0/tcp/12345")),
	)
	if err != nil {
		panic(err)
	}

	fmt.Println("\n Relay server peerID is", h1.ID().Pretty())
	fmt.Println("\n Relay server addresses are:")
	for _, a := range h1.Addrs() {
		fmt.Println(a)
	}

	select {
	case <-time.After(1 * time.Hour):
	}
}
