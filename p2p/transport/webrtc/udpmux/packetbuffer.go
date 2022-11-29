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

type packetBuffer struct {
	ctx         context.Context
	mu          sync.Mutex
	pdata       *pool.Buffer
	pkts        []packet
	notify      chan struct{}
	readWaiting bool
}

func newPacketBuffer(ctx context.Context) *packetBuffer {
	return &packetBuffer{
		ctx:    ctx,
		pdata:  new(pool.Buffer),
		notify: make(chan struct{}),
	}
}

func (pb *packetBuffer) readFrom(buf []byte) (int, net.Addr, error) {
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

func (pb *packetBuffer) writePacket(buf []byte, addr net.Addr) error {
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
