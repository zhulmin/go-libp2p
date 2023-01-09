package udpmux

import (
	"net"
	"net/netip"
	"testing"
	"time"

	pool "github.com/libp2p/go-buffer-pool"
	"github.com/stretchr/testify/require"
)

func TestPacketQueue_QueuePacketsForRead(t *testing.T) {
	b := newPacketQueue()
	b.push([]byte{1, 2, 3}, net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:1234")))
	b.push([]byte{5, 6, 7, 8}, net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:1235")))

	buf := pool.Get(100)
	size, _, err := b.pop(buf)
	require.NoError(t, err)
	require.Equal(t, size, 3)

	size, _, err = b.pop(buf)
	require.NoError(t, err)
	require.Equal(t, size, 4)
}

func TestPacketQueue_WaitsForData(t *testing.T) {
	pb := newPacketQueue()
	buf := pool.Get(100)

	timer := time.AfterFunc(200*time.Millisecond, func() {
		pb.push([]byte{5, 6, 7, 8}, net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:1235")))
	})

	defer timer.Stop()
	size, _, err := pb.pop(buf)
	require.NoError(t, err)
	require.Equal(t, size, 4)
}

func TestPacketQueue_DropsPacketsWhenQueueIsFull(t *testing.T) {
	pq := newPacketQueue()
	addr := net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:12345"))
	for i := 0; i < maxPacketsInQueue; i++ {
		buf := pool.Get(255)
		err := pq.push(buf, addr)
		require.NoError(t, err)
	}

	buf := pool.Get(255)
	err := pq.push(buf, addr)
	require.ErrorIs(t, err, errTooManyPackets)
}
