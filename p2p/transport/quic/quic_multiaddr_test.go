package libp2pquic

import (
	"net"
	"testing"

	"github.com/lucas-clemente/quic-go"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func TestConvertToQuicMultiaddr(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(192, 168, 0, 42), Port: 1337}
	maddr, err := toQuicMultiaddr(addr, quic.VersionDraft29)
	require.NoError(t, err)
	require.Equal(t, maddr.String(), "/ip4/192.168.0.42/udp/1337/quic")
}

func TestConvertToQuicV1Multiaddr(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(192, 168, 0, 42), Port: 1337}
	maddr, err := toQuicMultiaddr(addr, quic.Version1)
	require.NoError(t, err)
	require.Equal(t, maddr.String(), "/ip4/192.168.0.42/udp/1337/quic-v1")
}

func TestConvertFromQuicDraft29Multiaddr(t *testing.T) {
	maddr, err := ma.NewMultiaddr("/ip4/192.168.0.42/udp/1337/quic")
	require.NoError(t, err)
	addr, v, err := fromQuicMultiaddr(maddr)
	require.NoError(t, err)
	udpAddr, ok := addr.(*net.UDPAddr)
	require.True(t, ok)
	require.Equal(t, udpAddr.IP, net.IPv4(192, 168, 0, 42))
	require.Equal(t, udpAddr.Port, 1337)
	require.Equal(t, v, quic.VersionDraft29)
}

func TestConvertFromQuicV1Multiaddr(t *testing.T) {
	maddr, err := ma.NewMultiaddr("/ip4/192.168.0.42/udp/1337/quic-v1")
	require.NoError(t, err)
	addr, v, err := fromQuicMultiaddr(maddr)
	require.NoError(t, err)
	udpAddr, ok := addr.(*net.UDPAddr)
	require.True(t, ok)
	require.Equal(t, udpAddr.IP, net.IPv4(192, 168, 0, 42))
	require.Equal(t, udpAddr.Port, 1337)
	require.Equal(t, v, quic.Version1)
}
