package tor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/cretz/bine/tor"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/transport"

	logging "github.com/ipfs/go-log/v2"
	ma "github.com/multiformats/go-multiaddr"
	mafmt "github.com/multiformats/go-multiaddr-fmt"
	manet "github.com/multiformats/go-multiaddr/net"
)

const defaultConnectTimeout = 60 * time.Second

var log = logging.Logger("tor-tpt")

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

	tor    *tor.Tor
	dialer *tor.Dialer
}

var _ transport.Transport = &Transport{}

// NewTransport creates a tor transport object that dials connections over a Socks5 proxy
func NewTransport(btor *tor.Tor, upgrader transport.Upgrader, rcmgr network.ResourceManager, opts ...Option) (*Transport, error) {
	if rcmgr == nil {
		rcmgr = &network.NullResourceManager{}
	}
	tr := &Transport{
		upgrader:       upgrader,
		connectTimeout: defaultConnectTimeout, // can be set by using the WithConnectionTimeout option
		rcmgr:          rcmgr,
		tor:            btor,
	}
	for _, o := range opts {
		if err := o(tr); err != nil {
			return nil, err
		}
	}
	dialer, err := btor.Dialer(context.Background(), &tor.DialConf{})
	if err != nil {
		return nil, err
	}
	tr.dialer = dialer
	return tr, nil
}

var dialMatcher = mafmt.And(mafmt.IP, mafmt.Base(ma.P_TCP))

// CanDial returns true if this transport believes it can dial the given
// multiaddr.
func (t *Transport) CanDial(addr ma.Multiaddr) bool {
	if dialMatcher.Matches(addr) {
		return true
	}
	protocols := addr.Protocols()
	if len(protocols) == 0 {
		return false
	}
	return protocols[0].Code == ma.P_ONION3
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
	protocols := raddr.Protocols()
	if len(protocols) == 0 {
		return nil, errors.New("invalid addr")
	}
	var conn manet.Conn
	if os, err := raddr.ValueForProtocol(ma.P_ONION3); err == nil {
		v := strings.SplitN(os, ":", 2)
		c, err := t.dialer.DialContext(ctx, "tcp", v[0]+".onion:"+v[1])
		if err != nil {
			return nil, err
		}
		la, err := manet.FromNetAddr(c.LocalAddr())
		if err != nil {
			return nil, err
		}
		conn = &Conn{
			Conn:            c,
			localMultiaddr:  la,
			remoteMultiaddr: raddr,
		}
	} else {
		conn, err = t.maDial(ctx, raddr)
		if err != nil {
			return nil, err
		}
	}
	// Set linger to 0 so we never get stuck in the TIME-WAIT state. When
	// linger is 0, connections are _reset_ instead of closed with a FIN.
	// This means we can immediately reuse the 5-tuple and reconnect.
	tryLinger(conn, 0)
	c := conn
	direction := network.DirOutbound
	if ok, isClient, _ := network.GetSimultaneousConnect(ctx); ok && !isClient {
		direction = network.DirInbound
	}
	return t.upgrader.Upgrade(ctx, t, c, direction, p, connScope)
}

// Listen listens on the given multiaddr.
func (t *Transport) Listen(laddr ma.Multiaddr) (transport.Listener, error) {
	protocols := laddr.Protocols()
	if len(protocols) == 0 || protocols[0].Code != ma.P_ONION3 {
		return nil, errors.New("cannot listen on this transport")
	}
	as := laddr.String()
	port := 1337
	idx := strings.LastIndex(as, ":")
	if idx != -1 {
		var err error
		ss := as[idx+1:]
		if ss[len(ss)-1] == '/' {
			ss = ss[:len(ss)-1]
		}
		port, err = strconv.Atoi(ss)
		if err != nil {
			return nil, err
		}
	}

	svc, err := t.tor.Listen(context.Background(), &tor.ListenConf{
		LocalPort:   0,
		RemotePorts: []int{port},
	})
	if err != nil {
		return nil, err
	}
	laddr = ma.StringCast(fmt.Sprintf("/onion3/%s:%d", svc.ID, port))
	fmt.Println(laddr)
	upgL := t.upgrader.UpgradeListener(t,
		&Listener{
			service:        svc,
			id:             svc.ID,
			localMultiaddr: laddr,
		})
	return upgL, nil
}

// Protocols returns the list of terminal protocols this transport can dial.
func (t *Transport) Protocols() []int {
	return []int{ma.P_TCP, ma.P_ONION3}
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
