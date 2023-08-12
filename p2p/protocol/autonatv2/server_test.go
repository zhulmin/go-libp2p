package autonatv2

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/test"
	bhost "github.com/libp2p/go-libp2p/p2p/host/blank"
	swarmt "github.com/libp2p/go-libp2p/p2p/net/swarm/testing"
	"github.com/libp2p/go-libp2p/p2p/protocol/autonatv2/pbv2"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

// identify provides server address and protocol to client
func identify(cli *AutoNAT, srv *AutoNAT) {
	cli.host.Peerstore().AddAddrs(srv.host.ID(), srv.host.Addrs(), peerstore.PermanentAddrTTL)
	cli.host.Peerstore().AddProtocols(srv.host.ID(), DialProtocol)
}

func TestServerAllAddrsInvalid(t *testing.T) {
	dialer := bhost.NewBlankHost(swarmt.GenSwarm(t, swarmt.OptDisableQUIC, swarmt.OptDisableTCP))
	an := newAutoNAT(t, dialer, allowAll)
	defer an.Close()
	defer an.host.Close()
	an.srv.Enable()

	c := newAutoNAT(t, nil, allowAll)
	defer c.Close()
	defer c.host.Close()

	identify(c, an)

	res, err := c.CheckReachability(context.Background(), c.host.Addrs(), nil)
	require.NoError(t, err)
	for _, r := range res {
		require.Equal(t, r.Status, pbv2.DialStatus_E_TRANSPORT_NOT_SUPPORTED)
	}
}

func TestServerPrivateRejected(t *testing.T) {
	an := newAutoNAT(t, nil)
	defer an.Close()
	defer an.host.Close()
	an.srv.Enable()

	c := newAutoNAT(t, nil, allowAll)
	defer c.Close()
	defer c.host.Close()

	identify(c, an)

	res, err := c.CheckReachability(context.Background(), c.host.Addrs(), nil)
	require.NoError(t, err)
	for _, r := range res {
		require.Equal(t, r.Status, pbv2.DialStatus_E_DIAL_REFUSED)
	}
}

func TestServerDataRequest(t *testing.T) {
	dialer := bhost.NewBlankHost(swarmt.GenSwarm(t, swarmt.OptDisableTCP))
	an := newAutoNAT(t, dialer, allowAll, WithDataRequestPolicy(
		func(s network.Stream, dialAddr ma.Multiaddr) bool {
			if _, err := dialAddr.ValueForProtocol(ma.P_QUIC_V1); err == nil {
				return true
			}
			return false
		}),
		WithServerRateLimit(10, 10),
	)
	an.srv.Enable()
	defer an.host.Close()

	c := newAutoNAT(t, nil)
	c.allowAllAddrs = true
	defer c.Close()
	defer c.host.Close()

	identify(c, an)

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

	require.Equal(t, res[0].Reachability, network.ReachabilityPublic)
}

func TestServerDial(t *testing.T) {
	an := newAutoNAT(t, nil, WithServerRateLimit(10, 10), allowAll)
	defer an.host.Close()
	an.srv.Enable()

	c := newAutoNAT(t, nil, allowAll)
	defer c.Close()
	defer c.host.Close()

	identify(c, an)

	randAddr := ma.StringCast("/ip4/1.2.3.4/tcp/2")
	res, err := c.CheckReachability(context.Background(), []ma.Multiaddr{randAddr}, c.host.Addrs())
	require.NoError(t, err)
	require.Equal(t, res[0].Reachability, network.ReachabilityPrivate)

	res, err = c.CheckReachability(context.Background(), nil, c.host.Addrs())
	require.NoError(t, err)
	require.Equal(t, res[0].Reachability, network.ReachabilityPublic)
}

func TestRateLimiter(t *testing.T) {
	cl := test.NewMockClock()
	r := rateLimiter{RPM: 3, RPMPerPeer: 2, now: cl.Now}

	require.True(t, r.Accept("peer1"))

	cl.AdvanceBy(10 * time.Second)
	require.True(t, r.Accept("peer1"))

	cl.AdvanceBy(10 * time.Second)
	require.False(t, r.Accept("peer1"))

	cl.AdvanceBy(10 * time.Second)
	require.True(t, r.Accept("peer2"))

	cl.AdvanceBy(10 * time.Second)
	require.False(t, r.Accept("peer3"))

	cl.AdvanceBy(21 * time.Second) // first request expired
	require.True(t, r.Accept("peer1"))
}
