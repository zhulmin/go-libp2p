package tor_test

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/host/tor"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func TestConnectionToAM6(t *testing.T) {
	addr := ma.StringCast("/ip4/147.75.87.27/tcp/4001/")
	pid, err := peer.Decode("QmbLHAnMoJPWSCR5Zhtx6BHJX9KiKNN6tpvbUcqanj75Nb")
	require.NoError(t, err)

	h, err := tor.NewHost()
	require.NoError(t, err)
	defer h.Close()
	err = h.Connect(context.Background(), peer.AddrInfo{ID: pid, Addrs: []ma.Multiaddr{addr}})
	require.NoError(t, err)

}

func TestConnectionToBootstrap(t *testing.T) {
	addr := ma.StringCast("/dnsaddr/bootstrap.libp2p.io/p2p/QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN")
	pid, err := peer.Decode("QmNnooDu7bfjPFoTZYxMNLWUQJyrVwtbZg5gBMjTezGAJN")
	require.NoError(t, err)

	h, err := tor.NewHost()
	require.NoError(t, err)
	defer h.Close()
	err = h.Connect(context.Background(), peer.AddrInfo{ID: pid, Addrs: []ma.Multiaddr{addr}})
	require.NoError(t, err)
}

func TestListenOnion(t *testing.T) {
	h1, err := tor.NewHost()
	require.NoError(t, err)
	defer h1.Close()

	h2, err := tor.NewHost()
	require.NoError(t, err)

	h2.SetStreamHandler("/testprotocol", func(s network.Stream) {
		s.Write([]byte("Hello World!"))
		s.Close()
	})

	defer h2.Close()
	addr := "/onion3/4nnl24y3uvu65jag3napcvmgpt43d2yoqonn3cib5bhywrupsjvfr2id:80"
	err = h2.Network().Listen(ma.StringCast(addr))
	require.NoError(t, err)

	addrs := h2.Network().ListenAddresses()
	require.Equal(t, len(addrs), 1)

	err = h1.Connect(context.Background(), peer.AddrInfo{ID: h2.ID(), Addrs: addrs})
	require.NoError(t, err)

	s, err := h1.NewStream(context.Background(), h2.ID(), "/testprotocol")
	require.NoError(t, err)
	msg := make([]byte, 20)
	n, err := s.Read(msg)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	fmt.Println(string(msg[:n]))
}
