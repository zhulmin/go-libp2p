package main

import (
	"io"
	"log"
	mrand "math/rand"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
	libp2pquic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	libp2pwebrtc "github.com/libp2p/go-libp2p/p2p/transport/webrtc"
)

func main() {
	randReader := mrand.New(mrand.NewSource(1234))
	priv, _, err := crypto.GenerateEd25519Key(randReader)
	if err != nil {
		log.Fatal(err)
	}
	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.Transport(libp2pwebrtc.New),
		libp2p.Transport(libp2pquic.NewTransport),
		libp2p.ResourceManager(&network.NullResourceManager{}),
		libp2p.ListenAddrStrings(
			"/ip4/127.0.0.1/udp/1234/webrtc",
			"/ip4/127.0.0.1/udp/1235/quic",
		),
	)
	if err != nil {
		log.Fatal(err)
	}
	for _, addr := range h.Addrs() {
		log.Printf("I am %s/p2p/%s\n", addr, h.ID())
	}

	h.SetStreamHandler(protocol.TestingID, func(str network.Stream) {
		log.Printf("new stream from %s\n", str.Conn().RemotePeer())
		n, err := io.Copy(str, str)
		if err != nil {
			log.Printf("copy failed: %s\n", err)
			return
		}
		log.Println("copied", n)
		// don't close the stream, we _could_ send something later
	})

	select {}
}
