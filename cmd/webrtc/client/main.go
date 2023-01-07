package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"time"

	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	libp2pwebrtc "github.com/libp2p/go-libp2p/p2p/transport/webrtc"
)

func main() {
	h, err := libp2p.New(
		libp2p.Transport(libp2pwebrtc.New),
		libp2p.NoListenAddrs,
		libp2p.ResourceManager(&network.NullResourceManager{}),
	)
	if err != nil {
		log.Fatal(err)
	}
	pi, err := peer.AddrInfoFromString(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.Connect(ctx, *pi); err != nil {
		log.Fatal(err)
	}
	str, err := h.NewStream(ctx, pi.ID, protocol.TestingID)
	if err != nil {
		log.Fatal(err)
	}
	testdata := bytes.Repeat([]byte{0x42}, 4<<20) // ~4 MB of 0x42
	if _, err := str.Write(testdata); err != nil {
		log.Fatal(err)
	}
	if err := str.CloseWrite(); err != nil {
		log.Fatal(err)
	}
	data, err := io.ReadAll(str)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("received echo. matches:", bytes.Equal(data, testdata))
}
