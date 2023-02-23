package udpmux

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	pool "github.com/libp2p/go-buffer-pool"
)

var _ net.PacketConn = &muxedConnection{}

var (
	errAlreadyClosed = errors.New("already closed")
)

const maxPacketsInQueue = 128

// muxedConnection provides a net.PacketConn abstraction
// over packetQueue and adds the ability to store addresses
// from which this connection (indexed by ufrag) received
// data.
type muxedConnection struct {
	ctx     context.Context
	cancel  context.CancelFunc
	pq      *packetQueue
	ufrag   string
	address *string
	mux     *udpMux
}

func newMuxedConnection(mux *udpMux, ufrag string) *muxedConnection {
	ctx, cancel := context.WithCancel(mux.ctx)
	return &muxedConnection{
		ctx:    ctx,
		cancel: cancel,
		pq:     newPacketQueue(),
		ufrag:  ufrag,
		mux:    mux,
	}
}

func (conn *muxedConnection) GetAddress() (string, bool) {
	if conn.address == nil {
		return "", false
	}
	return *conn.address, true
}

func (conn *muxedConnection) SetAddress(s string) {
	if conn.address == nil {
		conn.address = &s
	} else if *conn.address != s {
		panic("address already set with differen value for same muxed conn")
	}
}

func (conn *muxedConnection) Push(buf []byte, addr net.Addr) error {
	return conn.pq.Push(buf, addr)
}

// Close implements net.PacketConn
func (conn *muxedConnection) Close() error {
	_ = conn.closeConnection()
	conn.mux.RemoveConnByUfrag(conn.ufrag)
	return nil
}

// LocalAddr implements net.PacketConn
func (conn *muxedConnection) LocalAddr() net.Addr {
	return conn.mux.socket.LocalAddr()
}

// ReadFrom implements net.PacketConn
func (conn *muxedConnection) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	return conn.pq.Pop(conn.ctx, p)
}

// SetDeadline implements net.PacketConn
func (*muxedConnection) SetDeadline(t time.Time) error {
	// no deadline is desired here
	return nil
}

// SetReadDeadline implements net.PacketConn
func (*muxedConnection) SetReadDeadline(t time.Time) error {
	// no read deadline is desired here
	return nil
}

// SetWriteDeadline implements net.PacketConn
func (*muxedConnection) SetWriteDeadline(t time.Time) error {
	// no write deadline is desired here
	return nil
}

// WriteTo implements net.PacketConn
func (conn *muxedConnection) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	return conn.mux.writeTo(p, addr)
}

func (conn *muxedConnection) closeConnection() error {
	select {
	case <-conn.ctx.Done():
		return errAlreadyClosed
	default:
	}
	conn.pq.Close()
	conn.cancel()
	return nil
}

type packet struct {
	addr net.Addr
	buf  []byte
}

var (
	errTooManyPackets    = errors.New("too many packets in queue; dropping")
	errPacketQueueClosed = errors.New("packet queue closed")
)

// just a convenience wrapper around a channel
type packetQueue struct {
	pktsMux sync.Mutex
	pktsCh  chan struct{}
	pkts    []packet

	ctx    context.Context
	cancel context.CancelFunc
}

func newPacketQueue() *packetQueue {
	ctx, cancel := context.WithCancel(context.Background())
	return &packetQueue{
		pktsCh: make(chan struct{}, maxPacketsInQueue),

		ctx:    ctx,
		cancel: cancel,
	}
}

// Pop reads a packet from the packetQueue or blocks until
// either a packet becomes available or the queue is closed.
func (pq *packetQueue) Pop(ctx context.Context, buf []byte) (int, net.Addr, error) {
	select {
	case <-pq.pktsCh:
		pq.pktsMux.Lock()
		defer pq.pktsMux.Unlock()

		p := pq.pkts[0]

		n := copy(buf, p.buf)
		if n == len(p.buf) {
			// only move packet from queue if we read all
			pq.pkts = pq.pkts[1:]
			pool.Put(p.buf)
		} else {
			// otherwise we need to keep the packet in the queue
			// but do update the buf
			pq.pkts[0].buf = p.buf[n:]
			// do make sure to put a receiver again
			select {
			case pq.pktsCh <- struct{}{}:
			default:
			}
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
	pq.pktsMux.Lock()
	defer pq.pktsMux.Unlock()

	if len(pq.pkts) >= maxPacketsInQueue {
		return errTooManyPackets
	}

	pq.pkts = append(pq.pkts, packet{addr, buf})
	select {
	case pq.pktsCh <- struct{}{}:
	default:
	}

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
