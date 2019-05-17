package pipetransport

import (
	"net"

	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr-net"
)

type PipeConn struct {
	net.Conn

	addr ma.Multiaddr
}

func WrapNetConn(conn net.Conn, addr ma.Multiaddr) *PipeConn {
	return &PipeConn{
		Conn: conn,
		addr: addr,
	}
}

func (c *PipeConn) LocalMultiaddr() ma.Multiaddr {
	return c.addr
}

func (c *PipeConn) RemoteMultiaddr() ma.Multiaddr {
	return c.addr
}

var _ manet.Conn = (*PipeConn)(nil)
