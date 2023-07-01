package udpmux

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPacketQueue_QueuePacketsForRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pq := newPacketQueue()
	pq.Push([]byte{1, 2, 3})
	pq.Push([]byte{5, 6, 7, 8})

	size, err := pq.Pop(ctx, make([]byte, 6))
	require.NoError(t, err)
	require.Equal(t, size, 3)

	size, err = pq.Pop(ctx, make([]byte, 6))
	require.NoError(t, err)
	require.Equal(t, size, 4)
}

func TestPacketQueue_WaitsForData(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pq := newPacketQueue()

	timer := time.AfterFunc(200*time.Millisecond, func() {
		pq.Push([]byte("foobar"))
	})

	defer timer.Stop()
	size, err := pq.Pop(ctx, make([]byte, 6))
	require.NoError(t, err)
	require.Equal(t, size, 6)
}

func TestPacketQueue_DropsPacketsWhenQueueIsFull(t *testing.T) {
	pq := newPacketQueue()
	for i := 0; i < maxPacketsInQueue; i++ {
		require.NoError(t, pq.Push(make([]byte, 10)))
	}
	require.ErrorIs(t, pq.Push(make([]byte, 10)), errTooManyPackets)
}
