package libp2pquic

import (
	"context"
	"net"

	"github.com/libp2p/go-libp2p/p2p/transport/udpreuse"

	ic "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	tpt "github.com/libp2p/go-libp2p/core/transport"
	p2ptls "github.com/libp2p/go-libp2p/p2p/security/tls"

	"github.com/lucas-clemente/quic-go"
	ma "github.com/multiformats/go-multiaddr"
)

var quicListen = quic.Listen // so we can mock it in tests

// A listener listens for QUIC connections.
type listener struct {
	quicListener   quic.Listener
	transport      *transport
	rcmgr          network.ResourceManager
	privKey        ic.PrivKey
	localPeer      peer.ID
	localMultiaddr ma.Multiaddr
}

var _ tpt.Listener = &listener{}

func newListener(ln udpreuse.Listener, t *transport, localPeer peer.ID, key ic.PrivKey, identity *p2ptls.Identity, rcmgr network.ResourceManager) (tpt.Listener, error) {
	localMultiaddr, err := toQuicMultiaddr(ln.Addr())
	if err != nil {
		return nil, err
	}
	return &listener{
		quicListener:   ln,
		transport:      t,
		rcmgr:          rcmgr,
		privKey:        key,
		localPeer:      localPeer,
		localMultiaddr: localMultiaddr,
	}, nil
}

// Accept accepts new connections.
func (l *listener) Accept() (tpt.CapableConn, error) {
	for {
		qconn, err := l.quicListener.Accept(context.Background())
		if err != nil {
			return nil, err
		}
		c, err := l.setupConn(qconn)
		if err != nil {
			qconn.CloseWithError(0, err.Error())
			continue
		}
		if l.transport.gater != nil && !(l.transport.gater.InterceptAccept(c) && l.transport.gater.InterceptSecured(network.DirInbound, c.remotePeerID, c)) {
			c.scope.Done()
			qconn.CloseWithError(errorCodeConnectionGating, "connection gated")
			continue
		}
		l.transport.addConn(qconn, c)

		// return through active hole punching if any
		key := holePunchKey{addr: qconn.RemoteAddr().String(), peer: c.remotePeerID}
		var wasHolePunch bool
		l.transport.holePunchingMx.Lock()
		holePunch, ok := l.transport.holePunching[key]
		if ok && !holePunch.fulfilled {
			holePunch.connCh <- c
			wasHolePunch = true
			holePunch.fulfilled = true
		}
		l.transport.holePunchingMx.Unlock()
		if wasHolePunch {
			continue
		}
		return c, nil
	}
}

func (l *listener) setupConn(qconn quic.Connection) (*conn, error) {
	remoteMultiaddr, err := toQuicMultiaddr(qconn.RemoteAddr())
	if err != nil {
		return nil, err
	}

	connScope, err := l.rcmgr.OpenConnection(network.DirInbound, false, remoteMultiaddr)
	if err != nil {
		log.Debugw("resource manager blocked incoming connection", "addr", qconn.RemoteAddr(), "error", err)
		return nil, err
	}
	// The tls.Config used to establish this connection already verified the certificate chain.
	// Since we don't have any way of knowing which tls.Config was used though,
	// we have to re-determine the peer's identity here.
	// Therefore, this is expected to never fail.
	remotePubKey, err := p2ptls.PubKeyFromCertChain(qconn.ConnectionState().TLS.PeerCertificates)
	if err != nil {
		connScope.Done()
		return nil, err
	}
	remotePeerID, err := peer.IDFromPublicKey(remotePubKey)
	if err != nil {
		connScope.Done()
		return nil, err
	}
	if err := connScope.SetPeer(remotePeerID); err != nil {
		log.Debugw("resource manager blocked incoming connection for peer", "peer", remotePeerID, "addr", qconn.RemoteAddr(), "error", err)
		connScope.Done()
		return nil, err
	}

	return &conn{
		quicConn:        qconn,
		transport:       l.transport,
		scope:           connScope,
		localPeer:       l.localPeer,
		localMultiaddr:  l.localMultiaddr,
		privKey:         l.privKey,
		remoteMultiaddr: remoteMultiaddr,
		remotePeerID:    remotePeerID,
		remotePubKey:    remotePubKey,
	}, nil
}

// Close closes the listener.
func (l *listener) Close() error {
	return l.quicListener.Close()
}

// Addr returns the address of this listener.
func (l *listener) Addr() net.Addr {
	return l.quicListener.Addr()
}

// Multiaddr returns the multiaddress of this listener.
func (l *listener) Multiaddr() ma.Multiaddr {
	return l.localMultiaddr
}
