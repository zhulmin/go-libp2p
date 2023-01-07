package main

import (
	"io"
	"log"
	mrand "math/rand"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
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
		libp2p.ResourceManager(&network.NullResourceManager{}),
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/1234/webrtc"),
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("I am %s/p2p/%s\n\n", h.Addrs()[0], h.ID())

	h.SetStreamHandler(protocol.TestingID, func(str network.Stream) {
		log.Printf("new stream from %s\n", str.Conn().RemotePeer())
		n, err := io.Copy(str, str)
		if err != nil {
			log.Printf("copy failed: %s\n", err)
			return
		}
		log.Println("copied", n)
		str.CloseWrite()
	})

	select {}
}
