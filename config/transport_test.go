package config

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"

	"github.com/stretchr/testify/require"
)

func TestTransportVariadicOptions(t *testing.T) {
	_, err := TransportConstructor(func(_ peer.ID, _ ...int) network.Transport { return nil })
	require.NoError(t, err)
}

func TestConstructorWithoutOptsCalledWithOpts(t *testing.T) {
	_, err := TransportConstructor(func(_ network.Upgrader) network.Transport {
		return nil
	}, 42)
	require.EqualError(t, err, "constructor doesn't accept any options")
}

func TestConstructorWithOptsTypeMismatch(t *testing.T) {
	_, err := TransportConstructor(func(_ network.Upgrader, opts ...int) network.Transport {
		return nil
	}, 42, "foo")
	require.EqualError(t, err, "expected option of type int, got string")
}

func TestConstructorWithOpts(t *testing.T) {
	var options []int
	c, err := TransportConstructor(func(_ network.Upgrader, opts ...int) (network.Transport, error) {
		options = opts
		return tcp.NewTCPTransport(nil, nil)
	}, 42, 1337)
	require.NoError(t, err)
	_, err = c(nil, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, []int{42, 1337}, options)
}
