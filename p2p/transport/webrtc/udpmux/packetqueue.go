package udpmux

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"

	pool "github.com/libp2p/go-buffer-pool"
)

const maxPacketsInQueue int = 128

type packet struct {
	addr net.Addr
	buf  []byte
}

var (
	errTooManyPackets = fmt.Errorf("too many packets in queue; dropping")
)

// packetQueue is a blocking queue for buffering packets received
// on net.PacketConn instances. Packets can be pushed by using
// `push` and read by using `pop`
type packetQueue struct {
	ctx         context.Context
	mu          sync.Mutex
	pkts        []packet
	notify      chan struct{}
	readWaiting bool
}

func newPacketQueue(ctx context.Context) *packetQueue {
	return &packetQueue{
		ctx:    ctx,
		notify: make(chan struct{}),
	}
}

// pop reads a packet from the packetQueue or blocks until
// either a packet becomes available or the buffer is closed.
func (pq *packetQueue) pop(buf []byte) (int, net.Addr, error) {
	for {
		pq.mu.Lock()

		if len(pq.pkts) != 0 {
			pkt := pq.pkts[0]
			defer pool.Put(pkt.buf[:cap(pkt.buf)])
			pq.pkts = pq.pkts[1:]
			pq.mu.Unlock()

			copied := copy(buf, pkt.buf)
			var err error
			if copied < len(pkt.buf) {
				err = io.ErrShortBuffer
			}

			return copied, pkt.addr, err
		}

		notify := pq.notify
		pq.readWaiting = true
		pq.mu.Unlock()

		select {
		case <-notify:
		case <-pq.ctx.Done():
			pq.mu.Lock()
			if len(pq.pkts) == 0 {
				pq.mu.Unlock()
				return 0, nil, io.EOF
			}
			pq.mu.Unlock()
		}
	}
}

// push adds a packet to the packetQueue
func (pq *packetQueue) push(buf []byte, addr net.Addr) error {
	select {
	case <-pq.ctx.Done():
		return io.ErrClosedPipe
	default:
	}
	pq.mu.Lock()
	if len(pq.pkts) == maxPacketsInQueue {
		// cleanup the packet and return
		defer pool.Put(buf[:cap(buf)])
		pq.mu.Unlock()
		return errTooManyPackets
	}
	pq.pkts = append(pq.pkts, packet{addr, buf})

	var notify chan struct{}
	if pq.readWaiting {
		pq.readWaiting = false
		notify = pq.notify
		pq.notify = make(chan struct{})
	}
	pq.mu.Unlock()
	if notify != nil {
		close(notify)
	}
	return nil
}

// discard all packets in the queue and return
// buffers
func (pq *packetQueue) close() {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	for _, pkt := range pq.pkts {
		buf := pkt.buf[:cap(pkt.buf)]
		pool.Put(buf)
	}
}
