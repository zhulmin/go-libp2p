package udpmux

import (
	"context"
	"net"
	"time"
)

var _ net.PacketConn = &muxedConnection{}

// muxedConnection provides a net.PacketConn abstraction
// over packetQueue and adds the ability to store addresses
// from which this connection (indexed by ufrag) received
// data.
type muxedConnection struct {
	ctx     context.Context
	cancel  context.CancelFunc
	onClose func()
	pq      *packetQueue
	addr    net.Addr
	mux     *udpMux
}

var _ net.PacketConn = (*muxedConnection)(nil)

func newMuxedConnection(mux *udpMux, onClose func(), addr net.Addr) *muxedConnection {
	ctx, cancel := context.WithCancel(mux.ctx)
	return &muxedConnection{
		ctx:     ctx,
		cancel:  cancel,
		pq:      newPacketQueue(),
		onClose: onClose,
		addr:    addr,
		mux:     mux,
	}
}

func (c *muxedConnection) Push(buf []byte) error {
	return c.pq.Push(buf)
}

// Close implements net.PacketConn
func (c *muxedConnection) Close() error {
	select {
	case <-c.ctx.Done():
		return nil
	default:
	}
	c.onClose()
	c.cancel()
	return nil
}

// LocalAddr implements net.PacketConn
func (c *muxedConnection) LocalAddr() net.Addr {
	return c.mux.socket.LocalAddr()
}

func (c *muxedConnection) Address() net.Addr {
	return c.addr
}

// ReadFrom implements net.PacketConn
func (c *muxedConnection) ReadFrom(p []byte) (int, net.Addr, error) {
	n, err := c.pq.Pop(c.ctx, p)
	return n, c.addr, err
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
func (c *muxedConnection) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	return c.mux.writeTo(p, addr)
}
