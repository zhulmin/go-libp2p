package udpmux

import (
	"context"
	"errors"
	"sync"

	pool "github.com/libp2p/go-buffer-pool"
)

type packet struct {
	buf []byte
}

var (
	errTooManyPackets    = errors.New("too many packets in queue; dropping")
	errPacketQueueClosed = errors.New("packet queue closed")
)

const maxPacketsInQueue = 128

type packetQueue struct {
	packetsMux sync.Mutex
	packetsCh  chan struct{}
	packets    []packet
}

func newPacketQueue() *packetQueue {
	return &packetQueue{
		packetsCh: make(chan struct{}, 1),
	}
}

// Pop reads a packet from the packetQueue.
func (pq *packetQueue) Pop(ctx context.Context, buf []byte) (int, error) {
start:
	select {
	case <-pq.packetsCh:
		pq.packetsMux.Lock()

		if len(pq.packets) == 0 {
			pq.packetsMux.Unlock()
			goto start
		}

		defer pq.packetsMux.Unlock()
		p := pq.packets[0]
		n := copy(buf, p.buf)
		if n < len(p.buf) {
			log.Debugf("short read, had %d, read %d", len(p.buf), n)
		}
		pq.packets = pq.packets[1:]
		pool.Put(p.buf)
		if len(pq.packets) > 0 {
			// to make sure a next pop call will work
			pq.notify()
		}
		return n, nil
	case <-ctx.Done():
		return 0, errPacketQueueClosed
	}
}

// Push adds a packet to the packetQueue
func (pq *packetQueue) Push(buf []byte) error {
	pq.packetsMux.Lock()
	defer pq.packetsMux.Unlock()

	if len(pq.packets) >= maxPacketsInQueue {
		return errTooManyPackets
	}

	pq.packets = append(pq.packets, packet{buf})
	pq.notify()
	return nil
}

func (pq *packetQueue) notify() {
	select {
	case pq.packetsCh <- struct{}{}:
	default:
	}
}
