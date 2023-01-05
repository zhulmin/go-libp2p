package websocket

import (
	"net"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/transport"
)

const maxReadAttempts = 5

type conn struct {
	net.Conn
	readAttempts uint8
	localAddr    addrWrapper
	remoteAddr   addrWrapper
}

var _ net.Conn = (conn)(conn{})

func (c conn) LocalAddr() net.Addr {
	return c.localAddr
}

func (c conn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

func (c conn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if err == nil && n == 0 && c.readAttempts < maxReadAttempts {
		c.readAttempts++
		// Nothing happened, let's read again.
		return c.Read(b)
	}
	return n, err
}

type capableConn struct {
	transport.CapableConn
}

func (c *capableConn) ConnState() network.ConnectionState {
	cs := c.CapableConn.ConnState()
	cs.Transport = "websocket"
	return cs
}
