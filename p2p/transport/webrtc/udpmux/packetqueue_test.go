package udpmux

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPacketQueue_QueuePacketsForRead(t *testing.T) {
	ctx := context.Background()
	b := newPacketQueue(ctx)
	b.push([]byte{1, 2, 3}, net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:1234")))
	b.push([]byte{5, 6, 7, 8}, net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:1235")))

	buf := make([]byte, 100)
	size, _, err := b.pop(buf)
	require.NoError(t, err)
	require.Equal(t, size, 3)

	size, _, err = b.pop(buf)
	require.NoError(t, err)
	require.Equal(t, size, 4)
}

func TestPacketQueue_WaitsForData(t *testing.T) {
	ctx := context.Background()
	pb := newPacketQueue(ctx)
	buf := make([]byte, 100)

	timer := time.AfterFunc(200*time.Millisecond, func() {
		pb.push([]byte{5, 6, 7, 8}, net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:1235")))
	})

	defer timer.Stop()
	size, _, err := pb.pop(buf)
	require.NoError(t, err)
	require.Equal(t, size, 4)
}
