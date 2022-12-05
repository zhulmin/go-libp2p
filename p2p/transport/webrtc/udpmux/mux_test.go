package udpmux

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var _ net.PacketConn = dummyPacketConn{}

type dummyPacketConn struct{}

// Close implements net.PacketConn
func (dummyPacketConn) Close() error {
	return nil
}

// LocalAddr implements net.PacketConn
func (dummyPacketConn) LocalAddr() net.Addr {
	return nil
}

// ReadFrom implements net.PacketConn
func (dummyPacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	return 0, &net.UDPAddr{}, nil
}

// SetDeadline implements net.PacketConn
func (dummyPacketConn) SetDeadline(t time.Time) error {
	return nil
}

// SetReadDeadline implements net.PacketConn
func (dummyPacketConn) SetReadDeadline(t time.Time) error {
	return nil
}

// SetWriteDeadline implements net.PacketConn
func (dummyPacketConn) SetWriteDeadline(t time.Time) error {
	return nil
}

// WriteTo implements net.PacketConn
func (dummyPacketConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	return 0, nil
}

func TestUDPMux_RemoveConnectionOnClose(t *testing.T) {
	mux := NewUDPMux(dummyPacketConn{}, nil)
	conn, err := mux.GetConn("test", false)
	require.NoError(t, err)
	require.NotNil(t, conn)

	m := mux.(*udpMux)
	require.NotNil(t, m.hasConn("test"))

	err = conn.Close()
	require.NoError(t, err)

	require.Nil(t, m.hasConn("test"))
}
