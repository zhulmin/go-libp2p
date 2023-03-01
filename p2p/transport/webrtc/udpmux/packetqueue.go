package udpmux

import (
	"context"
	"errors"
	"net"
	"sync"

	pool "github.com/libp2p/go-buffer-pool"
)

type packet struct {
	addr net.Addr
	buf  []byte
}

var (
	errTooManyPackets    = errors.New("too many packets in queue; dropping")
	errEmptyPacketQueue  = errors.New("packet queue is empty")
	errPacketQueueClosed = errors.New("packet queue closed")
)

// just a convenience wrapper around a channel
type packetQueue struct {
	packetsMux sync.Mutex
	packetsCh  chan struct{}
	packets    []packet

	ctx    context.Context
	cancel context.CancelFunc
}

func newPacketQueue() *packetQueue {
	ctx, cancel := context.WithCancel(context.Background())
	return &packetQueue{
		packetsCh: make(chan struct{}, maxPacketsInQueue),

		ctx:    ctx,
		cancel: cancel,
	}
}

// Pop reads a packet from the packetQueue or blocks until
// either a packet becomes available or the queue is closed.
func (pq *packetQueue) Pop(ctx context.Context, buf []byte) (int, net.Addr, error) {
	select {
	case <-pq.packetsCh:
		pq.packetsMux.Lock()
		defer pq.packetsMux.Unlock()

		if len(pq.packets) == 0 {
			return 0, nil, errEmptyPacketQueue
		}
		p := pq.packets[0]

		n := copy(buf, p.buf)
		if n == len(p.buf) {
			// only move packet from queue if we read all
			pq.packets = pq.packets[1:]
			pool.Put(p.buf)
		} else {
			// otherwise we need to keep the packet in the queue
			// but do update the buf
			pq.packets[0].buf = p.buf[n:]
			// do make sure to put a receiver again
			pq.packetsCh <- struct{}{}
		}

		return n, p.addr, nil

	// It is desired to allow reads of this channel even
	// when pq.ctx.Done() is already closed.
	case <-ctx.Done():
		return 0, nil, errPacketQueueClosed
	}
}

// Push adds a packet to the packetQueue
func (pq *packetQueue) Push(buf []byte, addr net.Addr) error {
	pq.packetsMux.Lock()
	defer pq.packetsMux.Unlock()

	if len(pq.packets) >= maxPacketsInQueue {
		return errTooManyPackets
	}

	pq.packets = append(pq.packets, packet{addr, buf})
	pq.packetsCh <- struct{}{}

	return nil
}

// discard all packets in the queue and return
// buffers
func (pq *packetQueue) Close() {
	select {
	case <-pq.ctx.Done():
	default:
		pq.cancel()
	}
}
