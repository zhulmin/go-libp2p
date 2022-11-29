package udpmux

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPacketBuffer_QueuePacketsForRead(t *testing.T) {
	ctx := context.Background()
	b := newPacketBuffer(ctx)
	b.writePacket([]byte{1, 2, 3}, net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:1234")))
	b.writePacket([]byte{5, 6, 7, 8}, net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:1235")))

	buf := make([]byte, 100)
	size, _, err := b.readFrom(buf)
	require.NoError(t, err)
	require.Equal(t, size, 3)

	size, _, err = b.readFrom(buf)
	require.NoError(t, err)
	require.Equal(t, size, 4)
}

func TestPacketBuffer_WaitsForData(t *testing.T) {
	ctx := context.Background()
	pb := newPacketBuffer(ctx)
	buf := make([]byte, 100)

	timer := time.AfterFunc(200*time.Millisecond, func() {
		pb.writePacket([]byte{5, 6, 7, 8}, net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:1235")))
	})

	defer timer.Stop()
	size, _, err := pb.readFrom(buf)
	require.NoError(t, err)
	require.Equal(t, size, 4)
}
