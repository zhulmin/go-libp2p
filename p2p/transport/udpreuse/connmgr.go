package udpreuse

import (
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync"

	"github.com/lucas-clemente/quic-go"
)

type Conn interface {
	net.PacketConn

	DecreaseCount()
	IncreaseCount()
}

type noreuseConn struct {
	*net.UDPConn
}

func (c *noreuseConn) IncreaseCount() {}
func (c *noreuseConn) DecreaseCount() {
	c.Close()
}

type ConnManager interface {
	ListenQUIC(network string, laddr *net.UDPAddr, proto string, tlsConf *tls.Config, allowWindowIncrease func(conn quic.Connection, delta uint64) bool) (Listener, error)
	Dial(network string, addr *net.UDPAddr) (Conn, error)
	io.Closer
}

type connManager struct {
	reuseUDP4       *reuse
	reuseUDP6       *reuse
	reuseportEnable bool

	mx    sync.Mutex
	conns map[string]*connListener
}

var _ ConnManager = &connManager{}

// NewConnManager creates a new reuse connection manager.
func NewConnManager(reuseport bool) (ConnManager, error) {
	reuseUDP4 := newReuse()
	reuseUDP6 := newReuse()
	return &connManager{
		reuseUDP4:       reuseUDP4,
		reuseUDP6:       reuseUDP6,
		reuseportEnable: reuseport,
		conns:           map[string]*connListener{},
	}, nil
}

func (c *connManager) getReuse(network string) (*reuse, error) {
	switch network {
	case "udp4":
		return c.reuseUDP4, nil
	case "udp6":
		return c.reuseUDP6, nil
	default:
		return nil, errors.New("invalid network: must be either udp4 or udp6")
	}
}

func (c *connManager) Listen(network string, laddr *net.UDPAddr) (Conn, error) {
	if c.reuseportEnable {
		reuse, err := c.getReuse(network)
		if err != nil {
			return nil, err
		}
		return reuse.Listen(network, laddr)
	}

	conn, err := net.ListenUDP(network, laddr)
	if err != nil {
		return nil, err
	}
	return &noreuseConn{conn}, nil
}

func (c *connManager) ListenQUIC(network string, laddr *net.UDPAddr, proto string, tlsConf *tls.Config, allowWindowIncrease func(conn quic.Connection, delta uint64) bool) (Listener, error) {
	c.mx.Lock()
	defer c.mx.Unlock()

	index := network + " " + laddr.String()
	ln, ok := c.conns[index]
	if !ok {
		conn, err := c.Listen(network, laddr)
		if err != nil {
			return nil, err
		}
		conn.IncreaseCount()
		ln, err = newConnListener(conn)
		if err != nil {
			return nil, err
		}
		// TODO: think about shutdown
		go func() {
			ln.Run()
			conn.DecreaseCount()
		}()
		c.conns[index] = ln
	}
	return ln.Add(proto, tlsConf, allowWindowIncrease)
}

func (c *connManager) Dial(network string, raddr *net.UDPAddr) (Conn, error) {
	if c.reuseportEnable {
		reuse, err := c.getReuse(network)
		if err != nil {
			return nil, err
		}
		return reuse.Dial(network, raddr)
	}

	var laddr *net.UDPAddr
	switch network {
	case "udp4":
		laddr = &net.UDPAddr{IP: net.IPv4zero, Port: 0}
	case "udp6":
		laddr = &net.UDPAddr{IP: net.IPv6zero, Port: 0}
	}
	conn, err := net.ListenUDP(network, laddr)
	if err != nil {
		return nil, err
	}
	return &noreuseConn{conn}, nil
}

func (c *connManager) Close() error {
	if err := c.reuseUDP6.Close(); err != nil {
		return err
	}
	return c.reuseUDP4.Close()
}
