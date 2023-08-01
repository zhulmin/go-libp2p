package tor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/transport"
	"golang.org/x/net/proxy"

	logging "github.com/ipfs/go-log/v2"
	ma "github.com/multiformats/go-multiaddr"
	mafmt "github.com/multiformats/go-multiaddr-fmt"
	manet "github.com/multiformats/go-multiaddr/net"
)

const defaultConnectTimeout = 60 * time.Second

var log = logging.Logger("tor-tpt")

const keepAlivePeriod = 30 * time.Second

type canKeepAlive interface {
	SetKeepAlive(bool) error
	SetKeepAlivePeriod(time.Duration) error
}

var _ canKeepAlive = &net.TCPConn{}

func tryKeepAlive(conn net.Conn, keepAlive bool) {
	keepAliveConn, ok := conn.(canKeepAlive)
	if !ok {
		log.Errorf("Can't set TCP keepalives.")
		return
	}
	if err := keepAliveConn.SetKeepAlive(keepAlive); err != nil {
		// Sometimes we seem to get "invalid argument" results from this function on Darwin.
		// This might be due to a closed connection, but I can't reproduce that on Linux.
		//
		// But there's nothing we can do about invalid arguments, so we'll drop this to a
		// debug.
		if errors.Is(err, os.ErrInvalid) || errors.Is(err, syscall.EINVAL) {
			log.Debugw("failed to enable TCP keepalive", "error", err)
		} else {
			log.Debugw("failed to enable TCP keepalive", "error", err)
		}
		return
	}

	if runtime.GOOS != "openbsd" {
		if err := keepAliveConn.SetKeepAlivePeriod(keepAlivePeriod); err != nil {
			log.Errorw("failed set keepalive period", "error", err)
		}
	}
}

// try to set linger on the connection, if possible.
func tryLinger(conn net.Conn, sec int) {
	type canLinger interface {
		SetLinger(int) error
	}

	if lingerConn, ok := conn.(canLinger); ok {
		_ = lingerConn.SetLinger(sec)
	}
}

type Option func(*Transport) error

func WithConnectionTimeout(d time.Duration) Option {
	return func(tr *Transport) error {
		tr.connectTimeout = d
		return nil
	}
}

func WithTorProxy(addr net.Addr) Option {
	return func(tr *Transport) error {
		tr.torProxy = addr
		return nil
	}
}

// Transport is the TCP transport.
type Transport struct {
	// Connection upgrader for upgrading insecure stream connections to
	// secure multiplex connections.
	upgrader transport.Upgrader

	// TCP connect timeout
	connectTimeout time.Duration

	rcmgr network.ResourceManager

	torProxy net.Addr

	dialer proxy.Dialer
}

var _ transport.Transport = &Transport{}

// NewTransport creates a tor transport object that dials connections over a Socks5 proxy
func NewTransport(upgrader transport.Upgrader, rcmgr network.ResourceManager, opts ...Option) (*Transport, error) {
	if rcmgr == nil {
		rcmgr = &network.NullResourceManager{}
	}
	tr := &Transport{
		upgrader:       upgrader,
		connectTimeout: defaultConnectTimeout, // can be set by using the WithConnectionTimeout option
		rcmgr:          rcmgr,
		torProxy:       &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9050},
	}
	for _, o := range opts {
		if err := o(tr); err != nil {
			return nil, err
		}
	}
	d, err := proxy.SOCKS5("tcp", tr.torProxy.String(), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("error connecting to tor proxy: %w", err)
	}
	tr.dialer = d
	return tr, nil
}

var dialMatcher = mafmt.And(mafmt.IP, mafmt.Base(ma.P_TCP))

// CanDial returns true if this transport believes it can dial the given
// multiaddr.
func (t *Transport) CanDial(addr ma.Multiaddr) bool {
	return dialMatcher.Matches(addr)
}

func (t *Transport) maDial(ctx context.Context, raddr ma.Multiaddr) (manet.Conn, error) {
	addr, err := manet.ToNetAddr(raddr)
	if err != nil {
		return nil, fmt.Errorf("failed to convert %s to net.Addr: %w", raddr, err)
	}
	conn, err := t.dialer.Dial(addr.Network(), addr.String())
	if err != nil {
		return nil, fmt.Errorf("dial to %s:%s failed: %w", addr.Network(), addr.String(), err)
	}
	laddr, _ := manet.FromNetAddr(t.torProxy)
	return &maconn{Conn: conn, localMultiaddr: laddr, remoteMultiaddr: raddr}, nil
}

// Dial dials the peer at the remote address.
func (t *Transport) Dial(ctx context.Context, raddr ma.Multiaddr, p peer.ID) (transport.CapableConn, error) {
	connScope, err := t.rcmgr.OpenConnection(network.DirOutbound, true, raddr)
	if err != nil {
		log.Debugw("resource manager blocked outgoing connection", "peer", p, "addr", raddr, "error", err)
		return nil, err
	}

	c, err := t.dialWithScope(ctx, raddr, p, connScope)
	if err != nil {
		connScope.Done()
		return nil, err
	}
	return c, nil
}

func (t *Transport) dialWithScope(ctx context.Context, raddr ma.Multiaddr, p peer.ID, connScope network.ConnManagementScope) (transport.CapableConn, error) {
	if err := connScope.SetPeer(p); err != nil {
		log.Debugw("resource manager blocked outgoing connection for peer", "peer", p, "addr", raddr, "error", err)
		return nil, err
	}
	conn, err := t.maDial(ctx, raddr)
	if err != nil {
		return nil, err
	}
	// Set linger to 0 so we never get stuck in the TIME-WAIT state. When
	// linger is 0, connections are _reset_ instead of closed with a FIN.
	// This means we can immediately reuse the 5-tuple and reconnect.
	tryLinger(conn, 0)
	tryKeepAlive(conn, true)
	c := conn
	direction := network.DirOutbound
	if ok, isClient, _ := network.GetSimultaneousConnect(ctx); ok && !isClient {
		direction = network.DirInbound
	}
	return t.upgrader.Upgrade(ctx, t, c, direction, p, connScope)
}

// Listen listens on the given multiaddr.
func (t *Transport) Listen(laddr ma.Multiaddr) (transport.Listener, error) {
	return nil, errors.New("cannot listen on tor transport")
}

// Protocols returns the list of terminal protocols this transport can dial.
func (t *Transport) Protocols() []int {
	return []int{ma.P_TCP}
}

// Proxy always returns false for the TCP transport.
func (t *Transport) Proxy() bool {
	return false
}

func (t *Transport) String() string {
	return "TOR"
}

type maconn struct {
	net.Conn
	localMultiaddr  ma.Multiaddr
	remoteMultiaddr ma.Multiaddr
}

// LocalMultiaddr implements manet.Conn.
func (m *maconn) LocalMultiaddr() ma.Multiaddr {
	return m.localMultiaddr
}

// RemoteMultiaddr implements manet.Conn.
func (m *maconn) RemoteMultiaddr() ma.Multiaddr {
	return m.remoteMultiaddr
}

var _ manet.Conn = (*maconn)(nil)
