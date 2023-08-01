package tor

import (
	"context"
	"fmt"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	blankhost "github.com/libp2p/go-libp2p/p2p/host/blank"
	swarmt "github.com/libp2p/go-libp2p/p2p/net/swarm/testing"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func TestConnectionToMars(t *testing.T) {
	addr := ma.StringCast("/ip4/18.118.145.25/tcp/4001/")
	pid, err := peer.Decode("12D3KooWGp8t5sthbWfEJ92yKYfmDHKjyxrmuXMBm24S6Mz4icHa")
	require.NoError(t, err)

	s := swarmt.GenSwarm(t, swarmt.OptDisableQUIC, swarmt.OptNoiseUpgrader, swarmt.OptDisableTCP, swarmt.OptTorTransport)
	bh := blankhost.NewBlankHost(s)
	h := &Host{BlankHost: bh}
	err = h.Connect(context.Background(), peer.AddrInfo{ID: pid, Addrs: []ma.Multiaddr{addr}})
	require.NoError(t, err)
	conns := h.Network().ConnsToPeer(pid)
	fmt.Println(conns[0].RemoteMultiaddr())
}
