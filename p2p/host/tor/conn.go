package tor

import (
	"net"

	ma "github.com/multiformats/go-multiaddr"
	multiaddr "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type Conn struct {
	net.Conn
	localMultiaddr  ma.Multiaddr
	remoteMultiaddr ma.Multiaddr
}

// LocalMultiaddr implements manet.Conn.
func (c *Conn) LocalMultiaddr() multiaddr.Multiaddr {
	return c.localMultiaddr
}

// RemoteMultiaddr implements manet.Conn.
func (c *Conn) RemoteMultiaddr() multiaddr.Multiaddr {
	return c.remoteMultiaddr
}

var _ manet.Conn = (*Conn)(nil)
