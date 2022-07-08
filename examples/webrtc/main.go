package main

import (
	"fmt"
	"log"

	"github.com/libp2p/go-libp2p"

	libp2pwebrtc "github.com/libp2p/go-libp2p/p2p/transport/webrtc"
)

func main() {
	fmt.Print("test")
	h1, err := libp2p.New(
		libp2p.Transport(libp2pwebrtc.New),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/udp/1234/webrtc"),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Server: %s/p2p/%s\n", h1.Addrs()[0], h1.ID())
	select {}
}
