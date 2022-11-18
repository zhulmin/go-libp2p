package libp2pquic

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"sync"
	"time"

	manet "github.com/multiformats/go-multiaddr/net"

	"golang.org/x/crypto/hkdf"

	"github.com/libp2p/go-libp2p/core/connmgr"
	ic "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/pnet"
	tpt "github.com/libp2p/go-libp2p/core/transport"
	p2ptls "github.com/libp2p/go-libp2p/p2p/security/tls"
	"github.com/libp2p/go-libp2p/p2p/transport/quicreuse"

	ma "github.com/multiformats/go-multiaddr"
	mafmt "github.com/multiformats/go-multiaddr-fmt"

	logging "github.com/ipfs/go-log/v2"
	"github.com/lucas-clemente/quic-go"
)

var log = logging.Logger("quic-transport")

var ErrHolePunching = errors.New("hole punching attempted; no active dial")

var HolePunchTimeout = 5 * time.Second

const statelessResetKeyInfo = "libp2p quic stateless reset key"
const errorCodeConnectionGating = 0x47415445 // GATE in ASCII

// The Transport implements the tpt.Transport interface for QUIC connections.
type transport struct {
	privKey     ic.PrivKey
	localPeer   peer.ID
	identity    *p2ptls.Identity
	connManager *quicreuse.ConnManager
	gater       connmgr.ConnectionGater
	rcmgr       network.ResourceManager

	holePunchingMx sync.Mutex
	holePunching   map[holePunchKey]*activeHolePunch

	enableDraft29 bool

	connMx sync.Mutex
	conns  map[quic.Connection]*conn
}

var _ tpt.Transport = &transport{}

type holePunchKey struct {
	addr string
	peer peer.ID
}

type activeHolePunch struct {
	connCh    chan tpt.CapableConn
	fulfilled bool
}

// NewTransport creates a new QUIC transport
func NewTransport(key ic.PrivKey, psk pnet.PSK, gater connmgr.ConnectionGater, rcmgr network.ResourceManager, opts ...Option) (tpt.Transport, error) {
	var cfg config
	if err := cfg.apply(opts...); err != nil {
		return nil, fmt.Errorf("unable to apply quic-tpt option(s): %w", err)
	}

	if len(psk) > 0 {
		log.Error("QUIC doesn't support private networks yet.")
		return nil, errors.New("QUIC doesn't support private networks yet")
	}
	localPeer, err := peer.IDFromPrivateKey(key)
	if err != nil {
		return nil, err
	}
	identity, err := p2ptls.NewIdentity(key)
	if err != nil {
		return nil, err
	}
	keyBytes, err := key.Raw()
	if err != nil {
		return nil, err
	}
	keyReader := hkdf.New(sha256.New, keyBytes, nil, []byte(statelessResetKeyInfo))
	var statelessResetKey quic.StatelessResetKey
	if _, err := io.ReadFull(keyReader, statelessResetKey[:]); err != nil {
		return nil, err
	}
	var reuseOpts []quicreuse.Option
	if cfg.metrics {
		reuseOpts = append(reuseOpts, quicreuse.EnableMetrics())
	}
	if cfg.disableReuseport {
		reuseOpts = append(reuseOpts, quicreuse.DisableReuseport())
	}
	if cfg.disableDraft29 {
		reuseOpts = append(reuseOpts, quicreuse.DisableDraft29())
	}
	connManager, err := quicreuse.NewConnManager(statelessResetKey, reuseOpts...)
	if err != nil {
		return nil, err
	}
	if rcmgr == nil {
		rcmgr = &network.NullResourceManager{}
	}

	return &transport{
		privKey:       key,
		localPeer:     localPeer,
		identity:      identity,
		connManager:   connManager,
		gater:         gater,
		rcmgr:         rcmgr,
		conns:         make(map[quic.Connection]*conn),
		holePunching:  make(map[holePunchKey]*activeHolePunch),
		enableDraft29: !cfg.disableDraft29,
	}, nil
}

// Dial dials a new QUIC connection
func (t *transport) Dial(ctx context.Context, raddr ma.Multiaddr, p peer.ID) (tpt.CapableConn, error) {
	tlsConf, keyCh := t.identity.ConfigForPeer(p)
	if ok, isClient, _ := network.GetSimultaneousConnect(ctx); ok && !isClient {
		return t.holePunch(ctx, raddr, p)
	}

	scope, err := t.rcmgr.OpenConnection(network.DirOutbound, false, raddr)
	if err != nil {
		log.Debugw("resource manager blocked outgoing connection", "peer", p, "addr", raddr, "error", err)
		return nil, err
	}
	if err := scope.SetPeer(p); err != nil {
		log.Debugw("resource manager blocked outgoing connection for peer", "peer", p, "addr", raddr, "error", err)
		scope.Done()
		return nil, err
	}
	pconn, err := t.connManager.DialQUIC(ctx, raddr, tlsConf, t.allowWindowIncrease)
	if err != nil {
		return nil, err
	}

	// Should be ready by this point, don't block.
	var remotePubKey ic.PubKey
	select {
	case remotePubKey = <-keyCh:
	default:
	}
	if remotePubKey == nil {
		pconn.CloseWithError(1, "")
		scope.Done()
		return nil, errors.New("p2p/transport/quic BUG: expected remote pub key to be set")
	}

	localMultiaddr, err := quicreuse.ToQuicMultiaddr(pconn.LocalAddr(), pconn.ConnectionState().Version)
	if err != nil {
		pconn.CloseWithError(1, "")
		return nil, err
	}
	c := &conn{
		quicConn:        pconn,
		transport:       t,
		scope:           scope,
		privKey:         t.privKey,
		localPeer:       t.localPeer,
		localMultiaddr:  localMultiaddr,
		remotePubKey:    remotePubKey,
		remotePeerID:    p,
		remoteMultiaddr: raddr,
	}
	if t.gater != nil && !t.gater.InterceptSecured(network.DirOutbound, p, c) {
		pconn.CloseWithError(errorCodeConnectionGating, "connection gated")
		return nil, fmt.Errorf("secured connection gated")
	}
	t.addConn(pconn, c)
	return c, nil
}

func (t *transport) addConn(conn quic.Connection, c *conn) {
	t.connMx.Lock()
	t.conns[conn] = c
	t.connMx.Unlock()
}

func (t *transport) removeConn(conn quic.Connection) {
	t.connMx.Lock()
	delete(t.conns, conn)
	t.connMx.Unlock()
}

func (t *transport) holePunch(ctx context.Context, raddr ma.Multiaddr, p peer.ID) (tpt.CapableConn, error) {
	network, saddr, err := manet.DialArgs(raddr)
	if err != nil {
		return nil, err
	}
	addr, err := net.ResolveUDPAddr(network, saddr)
	if err != nil {
		return nil, err
	}
	pconn, err := t.connManager.Dial(network, addr)
	if err != nil {
		return nil, err
	}
	defer pconn.DecreaseCount()

	ctx, cancel := context.WithTimeout(ctx, HolePunchTimeout)
	defer cancel()

	key := holePunchKey{addr: addr.String(), peer: p}
	t.holePunchingMx.Lock()
	if _, ok := t.holePunching[key]; ok {
		t.holePunchingMx.Unlock()
		return nil, fmt.Errorf("already punching hole for %s", addr)
	}
	connCh := make(chan tpt.CapableConn, 1)
	t.holePunching[key] = &activeHolePunch{connCh: connCh}
	t.holePunchingMx.Unlock()

	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	payload := make([]byte, 64)
	var punchErr error
loop:
	for i := 0; ; i++ {
		if _, err := rand.Read(payload); err != nil {
			punchErr = err
			break
		}
		if _, err := pconn.WriteTo(payload, addr); err != nil {
			punchErr = err
			break
		}

		maxSleep := 10 * (i + 1) * (i + 1) // in ms
		if maxSleep > 200 {
			maxSleep = 200
		}
		d := 10*time.Millisecond + time.Duration(rand.Intn(maxSleep))*time.Millisecond
		if timer == nil {
			timer = time.NewTimer(d)
		} else {
			timer.Reset(d)
		}
		select {
		case c := <-connCh:
			t.holePunchingMx.Lock()
			delete(t.holePunching, key)
			t.holePunchingMx.Unlock()
			return c, nil
		case <-timer.C:
		case <-ctx.Done():
			punchErr = ErrHolePunching
			break loop
		}
	}
	// we only arrive here if punchErr != nil
	t.holePunchingMx.Lock()
	defer func() {
		delete(t.holePunching, key)
		t.holePunchingMx.Unlock()
	}()
	select {
	case c := <-t.holePunching[key].connCh:
		return c, nil
	default:
		return nil, punchErr
	}
}

// Don't use mafmt.QUIC as we don't want to dial DNS addresses. Just /ip{4,6}/udp/quic
var dialMatcher = mafmt.And(mafmt.IP, mafmt.Base(ma.P_UDP), mafmt.Or(mafmt.Base(ma.P_QUIC), mafmt.Base(ma.P_QUIC_V1)))

// CanDial determines if we can dial to an address
func (t *transport) CanDial(addr ma.Multiaddr) bool {
	return dialMatcher.Matches(addr)
}

// Listen listens for new QUIC connections on the passed multiaddr.
func (t *transport) Listen(addr ma.Multiaddr) (tpt.Listener, error) {
	var tlsConf tls.Config
	tlsConf.GetConfigForClient = func(_ *tls.ClientHelloInfo) (*tls.Config, error) {
		// return a tls.Config that verifies the peer's certificate chain.
		// Note that since we have no way of associating an incoming QUIC connection with
		// the peer ID calculated here, we don't actually receive the peer's public key
		// from the key chan.
		conf, _ := t.identity.ConfigForPeer("")
		return conf, nil
	}
	tlsConf.NextProtos = []string{"libp2p"}

	ln, err := t.connManager.ListenQUIC(addr, &tlsConf, t.allowWindowIncrease)
	if err != nil {
		return nil, err
	}
	l, err := newListener(ln, t, t.localPeer, t.privKey, t.rcmgr, t.enableDraft29)
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	return l, nil
}

func (t *transport) allowWindowIncrease(conn quic.Connection, size uint64) bool {
	// If the QUIC connection tries to increase the window before we've inserted it
	// into our connections map (which we do right after dialing / accepting it),
	// we have no way to account for that memory. This should be very rare.
	// Block this attempt. The connection can request more memory later.
	t.connMx.Lock()
	c, ok := t.conns[conn]
	t.connMx.Unlock()
	if !ok {
		return false
	}
	return c.allowWindowIncrease(size)
}

// Proxy returns true if this transport proxies.
func (t *transport) Proxy() bool {
	return false
}

// Protocols returns the set of protocols handled by this transport.
func (t *transport) Protocols() []int {
	if t.enableDraft29 {
		return []int{ma.P_QUIC, ma.P_QUIC_V1}
	}
	return []int{ma.P_QUIC_V1}
}

func (t *transport) String() string {
	return "QUIC"
}

func (t *transport) Close() error {
	return t.connManager.Close()
}
