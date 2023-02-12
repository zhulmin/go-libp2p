package udpmux

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	pool "github.com/libp2p/go-buffer-pool"
)

var _ net.PacketConn = &muxedConnection{}

const maxPacketsInQueue int = 128

// muxedConnection provides a net.PacketConn abstraction
// over packetQueue and adds the ability to store addresses
// from which this connection (indexed by ufrag) received
// data.
type muxedConnection struct {
	ctx    context.Context
	cancel context.CancelFunc
	pq     *packetQueue
	// list of remote addresses associated with this connection.
	// this is useful as a mapping from [address] -> ufrag
	addresses []string
	ufrag     string
	mux       *udpMux
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
	pkts   chan packet
	ctx    context.Context
	cancel context.CancelFunc
}

func newPacketQueue() *packetQueue {
	ctx, cancel := context.WithCancel(context.Background())
	return &packetQueue{
		ctx:    ctx,
		cancel: cancel,
		pkts:   make(chan packet, maxPacketsInQueue),
	}
}

// Pop reads a packet from the packetQueue or blocks until
// either a packet becomes available or the buffer is closed.
func (pq *packetQueue) Pop(ctx context.Context, buf []byte) (int, net.Addr, error) {
	select {
	case <-ctx.Done():
		return 0, nil, errPacketQueueClosed
	// It is desired to allow reads of this channel even
	// when pq.ctx.Done() is already closed.
	case p, ok := <-pq.pkts:
		if !ok {
			return 0, nil, io.EOF
		}
		n := copy(buf, p.buf)
		var err error
		if n < len(p.buf) {
			err = io.ErrShortBuffer
		}
		p.buf = p.buf[:cap(p.buf)]
		pool.Put(p.buf)
		return n, p.addr, err
	}
}

// Push adds a packet to the packetQueue
func (pq *packetQueue) Push(buf []byte, addr net.Addr) (err error) {
	select {
	case pq.pkts <- packet{addr, buf}:
		return nil
	default:
		return errTooManyPackets
	}
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
