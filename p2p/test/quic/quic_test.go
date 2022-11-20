package quic_test

import (
	"context"
	"testing"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2pquic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/libp2p/go-libp2p/p2p/transport/quicreuse"
	webtransport "github.com/libp2p/go-libp2p/p2p/transport/webtransport"

	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func getQUICMultiaddrCode(addr ma.Multiaddr) int {
	if _, err := addr.ValueForProtocol(ma.P_QUIC); err == nil {
		return ma.P_QUIC
	}
	if _, err := addr.ValueForProtocol(ma.P_QUIC_V1); err == nil {
		return ma.P_QUIC_V1
	}
	return 0
}

func TestQUICVersions(t *testing.T) {
	h1, err := libp2p.New(
		libp2p.Transport(libp2pquic.NewTransport),
		libp2p.Transport(webtransport.New),
		libp2p.ListenAddrStrings(
			"/ip4/127.0.0.1/udp/12345/quic",    // QUIC draft-29
			"/ip4/127.0.0.1/udp/12345/quic-v1", // QUIC v1
		),
	)
	require.NoError(t, err)
	defer h1.Close()

	addrs := h1.Addrs()
	require.Len(t, addrs, 2)
	var quicDraft29Addr, quicV1Addr ma.Multiaddr
	for _, addr := range addrs {
		switch getQUICMultiaddrCode(addr) {
		case ma.P_QUIC:
			quicDraft29Addr = addr
		case ma.P_QUIC_V1:
			quicV1Addr = addr
		}
	}
	require.NotNil(t, quicDraft29Addr, "expected to be listening on a QUIC draft-29 address")
	require.NotNil(t, quicV1Addr, "expected to be listening on a QUIC v1 address")

	//  connect using QUIC draft-29
	h2, err := libp2p.New(
		libp2p.Transport(libp2pquic.NewTransport),
	)
	require.NoError(t, err)
	require.NoError(t, h2.Connect(context.Background(), peer.AddrInfo{ID: h1.ID(), Addrs: []ma.Multiaddr{quicDraft29Addr}}))
	conns := h2.Network().ConnsToPeer(h1.ID())
	require.Len(t, conns, 1)
	require.Equal(t, ma.P_QUIC, getQUICMultiaddrCode(conns[0].LocalMultiaddr()))
	require.Equal(t, ma.P_QUIC, getQUICMultiaddrCode(conns[0].RemoteMultiaddr()))
	h2.Close()

	//  connect using QUIC v1
	h3, err := libp2p.New(
		libp2p.Transport(libp2pquic.NewTransport),
	)
	require.NoError(t, err)
	require.NoError(t, h3.Connect(context.Background(), peer.AddrInfo{ID: h1.ID(), Addrs: []ma.Multiaddr{quicV1Addr}}))
	conns = h3.Network().ConnsToPeer(h1.ID())
	require.Len(t, conns, 1)
	require.Equal(t, ma.P_QUIC_V1, getQUICMultiaddrCode(conns[0].LocalMultiaddr()))
	require.Equal(t, ma.P_QUIC_V1, getQUICMultiaddrCode(conns[0].RemoteMultiaddr()))
	h3.Close()
}

func TestDisableQUICDraft29(t *testing.T) {
	h1, err := libp2p.New(
		libp2p.QUICReuse(quicreuse.NewConnManager, quicreuse.DisableDraft29()),
		libp2p.Transport(libp2pquic.NewTransport),
		libp2p.Transport(webtransport.New),
		libp2p.ListenAddrStrings(
			"/ip4/127.0.0.1/udp/12346/quic",    // QUIC draft-29
			"/ip4/127.0.0.1/udp/12346/quic-v1", // QUIC v1
		),
	)
	require.NoError(t, err)
	defer h1.Close()

	addrs := h1.Addrs()
	require.Len(t, addrs, 1)
	require.Equal(t, ma.P_QUIC_V1, getQUICMultiaddrCode(addrs[0]))

	//  connect using QUIC draft-29
	h2, err := libp2p.New(
		libp2p.Transport(libp2pquic.NewTransport),
	)
	require.NoError(t, err)
	defer h2.Close()
	require.ErrorContains(t,
		h2.Connect(context.Background(), peer.AddrInfo{ID: h1.ID(), Addrs: []ma.Multiaddr{ma.StringCast("/ip4/127.0.0.1/udp/12346/quic")}}),
		"no compatible QUIC version found",
	)
	// make sure that dialing QUIC v1 works
	require.NoError(t, h2.Connect(context.Background(), peer.AddrInfo{ID: h1.ID(), Addrs: []ma.Multiaddr{addrs[0]}}))
}

func TestQUICAndWebTransport(t *testing.T) {
	h1, err := libp2p.New(
		libp2p.QUICReuse(quicreuse.NewConnManager, quicreuse.DisableDraft29()),
		libp2p.Transport(libp2pquic.NewTransport),
		libp2p.Transport(webtransport.New),
		libp2p.ListenAddrStrings(
			"/ip4/127.0.0.1/udp/12347/quic-v1",
			"/ip4/127.0.0.1/udp/12347/quic-v1/webtransport",
		),
	)
	require.NoError(t, err)
	defer h1.Close()

	addrs := h1.Addrs()
	require.Len(t, addrs, 2)
	require.Equal(t, ma.P_QUIC_V1, getQUICMultiaddrCode(addrs[0]))
	require.Equal(t, ma.P_QUIC_V1, getQUICMultiaddrCode(addrs[1]))
	var quicAddr, webtransportAddr ma.Multiaddr
	for _, addr := range addrs {
		if _, err := addr.ValueForProtocol(ma.P_WEBTRANSPORT); err == nil {
			webtransportAddr = addr
		} else {
			quicAddr = addr
		}
	}
	require.NotNil(t, webtransportAddr, "expected to have a WebTransport address")
	require.NotNil(t, quicAddr, "expected to have a QUIC v1 address")

	h2, err := libp2p.New(
		libp2p.Transport(libp2pquic.NewTransport),
		libp2p.NoListenAddrs,
	)
	require.NoError(t, err)
	require.NoError(t, h2.Connect(context.Background(), peer.AddrInfo{ID: h1.ID(), Addrs: h1.Addrs()}))
	for _, conns := range [][]network.Conn{h2.Network().ConnsToPeer(h1.ID()), h1.Network().ConnsToPeer(h2.ID())} {
		require.Len(t, conns, 1)
		if _, err := conns[0].LocalMultiaddr().ValueForProtocol(ma.P_WEBTRANSPORT); err == nil {
			t.Fatalf("expected a QUIC connection, got a WebTransport connection (%s <-> %s)", conns[0].LocalMultiaddr(), conns[0].RemoteMultiaddr())
		}
		require.Equal(t, ma.P_QUIC_V1, getQUICMultiaddrCode(conns[0].LocalMultiaddr()))
		require.Equal(t, ma.P_QUIC_V1, getQUICMultiaddrCode(conns[0].RemoteMultiaddr()))
	}
	h2.Close()

	h3, err := libp2p.New(
		libp2p.Transport(webtransport.New),
		libp2p.NoListenAddrs,
	)
	require.NoError(t, err)
	require.NoError(t, h3.Connect(context.Background(), peer.AddrInfo{ID: h1.ID(), Addrs: h1.Addrs()}))
	for _, conns := range [][]network.Conn{h3.Network().ConnsToPeer(h1.ID()), h1.Network().ConnsToPeer(h3.ID())} {
		require.Len(t, conns, 1)
		if _, err := conns[0].LocalMultiaddr().ValueForProtocol(ma.P_WEBTRANSPORT); err != nil {
			t.Fatalf("expected a WebTransport connection, got a QUIC connection (%s <-> %s)", conns[0].LocalMultiaddr(), conns[0].RemoteMultiaddr())
		}
		require.Equal(t, ma.P_QUIC_V1, getQUICMultiaddrCode(conns[0].LocalMultiaddr()))
		require.Equal(t, ma.P_QUIC_V1, getQUICMultiaddrCode(conns[0].RemoteMultiaddr()))
	}
	h3.Close()
}
