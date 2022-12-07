package udpmux

import (
	"context"
	"io"
	"net"
	"sync"

	pool "github.com/libp2p/go-buffer-pool"
)

type packet struct {
	addr net.Addr
	size int
}

// packetQueue is a blocking queue for buffering packets received
// on net.PacketConn instances. Packets can be pushed by using
// `push` and read by using `pop`
type packetQueue struct {
	ctx         context.Context
	mu          sync.Mutex
	pdata       *pool.Buffer
	pkts        []packet
	notify      chan struct{}
	readWaiting bool
}

func newPacketQueue(ctx context.Context) *packetQueue {
	return &packetQueue{
		ctx:    ctx,
		pdata:  new(pool.Buffer),
		notify: make(chan struct{}),
	}
}

// pop reads a packet from the packetBuffer or blocks until
// either a packet becomes available or the buffer is closed.
func (pb *packetQueue) pop(buf []byte) (int, net.Addr, error) {
	for {
		pb.mu.Lock()

		if len(pb.pkts) != 0 {
			pkt := pb.pkts[0]
			pktdata := pb.pdata.Next(pkt.size)
			pb.pkts = pb.pkts[1:]
			copied := copy(buf, pktdata)
			if copied < len(pktdata) {
				pb.mu.Unlock()
				return copied, pkt.addr, io.ErrShortBuffer
			}

			pb.mu.Unlock()
			return copied, pkt.addr, nil
		}

		notify := pb.notify

		pb.readWaiting = true
		pb.mu.Unlock()

		select {
		case <-notify:
		case <-pb.ctx.Done():
			pb.mu.Lock()
			if len(pb.pkts) == 0 {
				pb.pdata.Reset()
				pb.mu.Unlock()
				return 0, nil, io.EOF
			}
			pb.mu.Unlock()
		}
	}
}

// push adds a packet to the packetBuffer
func (pb *packetQueue) push(buf []byte, addr net.Addr) error {
	select {
	case <-pb.ctx.Done():
		return io.ErrClosedPipe
	default:
	}
	pb.mu.Lock()
	_, err := pb.pdata.Write(buf)
	if err != nil {
		return err
	}
	pb.pkts = append(pb.pkts, packet{addr: addr, size: len(buf)})

	var notify chan struct{}
	if pb.readWaiting {
		pb.readWaiting = false
		notify = pb.notify
		pb.notify = make(chan struct{})
	}
	pb.mu.Unlock()
	if notify != nil {
		close(notify)
	}
	return nil
}
