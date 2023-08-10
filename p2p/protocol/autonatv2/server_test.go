package autonatv2

import (
	"context"
	"testing"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peerstore"
	bhost "github.com/libp2p/go-libp2p/p2p/host/blank"
	swarmt "github.com/libp2p/go-libp2p/p2p/net/swarm/testing"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func TestServerAllAddrsInvalid(t *testing.T) {
	h := bhost.NewBlankHost(swarmt.GenSwarm(t))
	dialer := bhost.NewBlankHost(swarmt.GenSwarm(t, swarmt.OptDisableQUIC, swarmt.OptDisableTCP))
	as, err := New(h, dialer)
	require.NoError(t, err)
	defer as.Close()
	defer as.host.Close()

	as.srv.Start()

	c := newAutoNAT(t)
	c.allowAllAddrs = true
	defer c.Close()
	defer c.host.Close()

	c.host.Peerstore().AddAddrs(as.host.ID(), as.host.Addrs(), peerstore.PermanentAddrTTL)
	c.host.Peerstore().AddProtocols(as.host.ID(), DialProtocol)

	res, err := c.CheckReachability(context.Background(), c.host.Addrs(), nil)
	require.NoError(t, err)
	for _, r := range res {
		require.Error(t, r.Err)
	}
}

func TestServerPrivateRejected(t *testing.T) {
	h := bhost.NewBlankHost(swarmt.GenSwarm(t))
	dialer := bhost.NewBlankHost(swarmt.GenSwarm(t))
	as, err := New(h, dialer)
	require.NoError(t, err)
	defer as.Close()
	defer as.host.Close()

	as.srv.Start()

	c := newAutoNAT(t)
	c.allowAllAddrs = true
	defer c.Close()
	defer c.host.Close()

	c.host.Peerstore().AddAddrs(as.host.ID(), as.host.Addrs(), peerstore.PermanentAddrTTL)
	c.host.Peerstore().AddProtocols(as.host.ID(), DialProtocol)

	res, err := c.CheckReachability(context.Background(), c.host.Addrs(), nil)
	require.NoError(t, err)
	for _, r := range res {
		require.Error(t, r.Err)
	}
}

func TestServerDataRequest(t *testing.T) {
	h := bhost.NewBlankHost(swarmt.GenSwarm(t))
	dialer := bhost.NewBlankHost(swarmt.GenSwarm(t, swarmt.OptDisableTCP))
	as := NewServer(h, dialer, func(s network.Stream, dialAddr ma.Multiaddr) bool {
		if _, err := dialAddr.ValueForProtocol(ma.P_QUIC_V1); err == nil {
			return true
		}
		return false
	}, true)
	defer as.host.Close()
	as.Start()

	c := newAutoNAT(t)
	c.allowAllAddrs = true
	defer c.Close()
	defer c.host.Close()

	c.host.Peerstore().AddAddrs(as.host.ID(), as.host.Addrs(), peerstore.PermanentAddrTTL)
	c.host.Peerstore().AddProtocols(as.host.ID(), DialProtocol)
	var quicAddr, tcpAddr ma.Multiaddr
	for _, a := range c.host.Addrs() {
		if _, err := a.ValueForProtocol(ma.P_QUIC_V1); err == nil {
			quicAddr = a
		} else if _, err := a.ValueForProtocol(ma.P_TCP); err == nil {
			tcpAddr = a
		}
	}

	_, err := c.CheckReachability(context.Background(), []ma.Multiaddr{tcpAddr}, []ma.Multiaddr{quicAddr})
	require.Error(t, err)

	res, err := c.CheckReachability(context.Background(), []ma.Multiaddr{quicAddr}, []ma.Multiaddr{tcpAddr})
	require.NoError(t, err)

	require.Equal(t, res[0].Rch, network.ReachabilityPublic)
}

func TestServerDial(t *testing.T) {
	h := bhost.NewBlankHost(swarmt.GenSwarm(t))
	dialer := bhost.NewBlankHost(swarmt.GenSwarm(t))
	as := NewServer(h, dialer, nil, true)
	defer as.host.Close()
	as.Start()

	c := newAutoNAT(t)
	c.allowAllAddrs = true
	defer c.Close()
	defer c.host.Close()

	c.host.Peerstore().AddAddrs(as.host.ID(), as.host.Addrs(), peerstore.PermanentAddrTTL)
	c.host.Peerstore().AddProtocols(as.host.ID(), DialProtocol)
	randAddr := ma.StringCast("/ip4/1.2.3.4/tcp/2")
	res, err := c.CheckReachability(context.Background(), []ma.Multiaddr{randAddr}, c.host.Addrs())
	require.NoError(t, err)
	require.Equal(t, res[0].Rch, network.ReachabilityPrivate)

	res, err = c.CheckReachability(context.Background(), nil, c.host.Addrs())
	require.NoError(t, err)
	require.Equal(t, res[0].Rch, network.ReachabilityPublic)
}
