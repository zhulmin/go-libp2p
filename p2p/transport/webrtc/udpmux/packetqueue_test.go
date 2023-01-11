package udpmux

import (
	"context"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	pool "github.com/libp2p/go-buffer-pool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPacketQueue_QueuePacketsForRead(t *testing.T) {
	pq := newPacketQueue()
	pq.push([]byte{1, 2, 3}, net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:1234")))
	pq.push([]byte{5, 6, 7, 8}, net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:1235")))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	buf := pool.Get(100)
	size, _, err := pq.pop(ctx, buf)
	require.NoError(t, err)
	require.Equal(t, size, 3)

	size, _, err = pq.pop(ctx, buf)
	require.NoError(t, err)
	require.Equal(t, size, 4)
}

func TestPacketQueue_WaitsForData(t *testing.T) {
	pq := newPacketQueue()
	buf := pool.Get(100)

	timer := time.AfterFunc(200*time.Millisecond, func() {
		pq.push([]byte{5, 6, 7, 8}, net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:1235")))
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer timer.Stop()
	size, _, err := pq.pop(ctx, buf)
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

func TestPacketQueue_ReadAfterClose(t *testing.T) {
	pq := newPacketQueue()
	addr := net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:12345"))
	for i := 0; i < 1; i++ {
		buf := pool.Get(255)
		err := pq.push(buf, addr)
		require.NoError(t, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pq.close()
	_, _, err := pq.pop(ctx, pool.Get(255))
	require.NoError(t, err)
	pq.pop(ctx, pool.Get(255))
}

// this is to test for race conditions when closing
func TestCloseWhileSending(t *testing.T) {
	pq := newPacketQueue()
	addr := net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:12345"))
	done := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			buf := pool.Get(255)
			err := pq.push(buf, addr)
			if err != nil {
				assert.ErrorIs(t, err, io.ErrClosedPipe)
				close(done)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	time.AfterFunc(100*time.Millisecond, func() { pq.close() })
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.FailNow()
	}
}
