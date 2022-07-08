package main

import (
	"context"
	"log"
	"os"

	"github.com/libp2p/go-libp2p"

	"github.com/libp2p/go-libp2p/core/peer"
	libp2pwebrtc "github.com/libp2p/go-libp2p/p2p/transport/webrtc"
)



func main() {
	h2, err := libp2p.New(
		libp2p.NoListenAddrs,
		libp2p.Transport(libp2pwebrtc.New),
	)
	if err != nil {
		log.Fatal(err)
	}

	ai, err := peer.AddrInfoFromString(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	if err := h2.Connect(context.Background(), *ai); err != nil {
		log.Fatal(err)
	}
	log.Print("connected")
	conn := h2.Network().ConnsToPeer(ai.ID)[0]
	str, err := conn.NewStream(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	_, err = str.Write([]byte("foo"))
	if err != nil {
		log.Fatal(err)
	}
	select {}
}
