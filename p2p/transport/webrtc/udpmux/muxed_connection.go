package udpmux

import (
	"context"
	"net"
	"time"
)

var _ net.PacketConn = &muxedConnection{}

type muxedConnection struct {
	ctx    context.Context
	cancel context.CancelFunc
	buffer *packetBuffer
	// list of remote addresses associated with this connection.
	// this is useful as a mapping from [address] -> ufrag
	addresses []string
	ufrag string
	mux    *udpMux
}

func newMuxedConnection(mux *udpMux, ufrag string) *muxedConnection {
	ctx, cancel := context.WithCancel(context.Background())
	return &muxedConnection{
		ctx: ctx,
		cancel: cancel,
		buffer: newPacketBuffer(ctx),
		ufrag: ufrag,
		mux: mux,
	}
}

func (conn *muxedConnection) push(buf []byte, addr net.Addr) error {
	return conn.buffer.writePacket(buf, addr)
}

// Close implements net.PacketConn
func (conn *muxedConnection) Close() error {
	select {
	case <- conn.ctx.Done():
		return nil
	default:
	}
	conn.cancel()
	conn.mux.RemoveConnByUfrag(conn.ufrag)
	return nil
}

// LocalAddr implements net.PacketConn
func (conn *muxedConnection) LocalAddr() net.Addr {
	return conn.mux.socket.LocalAddr()
}

// ReadFrom implements net.PacketConn
func (conn *muxedConnection) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	return conn.buffer.readFrom(p)
}

// SetDeadline implements net.PacketConn
func (*muxedConnection) SetDeadline(t time.Time) error {
	return nil
}

// SetReadDeadline implements net.PacketConn
func (*muxedConnection) SetReadDeadline(t time.Time) error {
	return nil
}

// SetWriteDeadline implements net.PacketConn
func (*muxedConnection) SetWriteDeadline(t time.Time) error {
	return nil
}

// WriteTo implements net.PacketConn
func (conn *muxedConnection) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	return conn.mux.writeTo(p, addr)
}
