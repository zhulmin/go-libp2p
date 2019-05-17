package memorytransport

import (
	"fmt"
	"net"

	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr-net"
)

// MemoryListener listens on a channel for incoming connections.
type MemoryListener struct {
	listenaddr ma.Multiaddr
	listench   chan *MemoryConn
	transport  *MemoryTransport
}

func NewMemoryListener(addr ma.Multiaddr, ch chan *MemoryConn, t *MemoryTransport) *MemoryListener {
	return &MemoryListener{
		listenaddr: addr,
		listench:   ch,
		transport:  t,
	}
}

func (l *MemoryListener) Accept() (manet.Conn, error) {
	conn, ok := <-l.listench
	if !ok {
		return nil, fmt.Errorf("memorytransport closed")
	}
	return conn, nil
}

func (l *MemoryListener) Close() error {
	l.transport.closeListener(l.listenaddr.String())
	return nil
}

func (l *MemoryListener) Addr() net.Addr {
	return nil
}

func (l *MemoryListener) Multiaddr() ma.Multiaddr {
	return l.listenaddr
}

var _ manet.Listener = (*MemoryListener)(nil)
