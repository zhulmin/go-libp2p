package tor

import (
	"net"

	"github.com/cretz/bine/tor"
	ma "github.com/multiformats/go-multiaddr"
	multiaddr "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type Listener struct {
	service        *tor.OnionService
	id             string
	localMultiaddr ma.Multiaddr
}

func (l *Listener) Accept() (manet.Conn, error) {
	conn, err := l.service.Accept()
	if err != nil {
		return nil, err
	}
	return &maconn{
		Conn:            conn,
		localMultiaddr:  l.localMultiaddr,
		remoteMultiaddr: ma.StringCast("/ip4/0.0.0.0/tcp/1"),
	}, nil
}

func (l *Listener) Addr() net.Addr {
	return l
}

func (l *Listener) Close() error {
	l.service.Close()
	return nil
}

func (l *Listener) Multiaddr() multiaddr.Multiaddr {
	return l.localMultiaddr
}

func (l *Listener) Network() string {
	return "onion3"
}

func (l *Listener) String() string {
	return l.id + ".onion"
}

var _ manet.Listener = (*Listener)(nil)
