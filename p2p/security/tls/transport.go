package libp2ptls

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime/debug"

	"github.com/libp2p/go-libp2p/core/canonicallog"
	ci "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/sec"

	manet "github.com/multiformats/go-multiaddr/net"
)

// ID is the protocol ID (used when negotiating with multistream)
const ID = "/tls/1.0.0"

// Transport constructs secure communication sessions for a peer.
type Transport struct {
	identity *Identity

	localPeer peer.ID
	privKey   ci.PrivKey
}

// New creates a TLS encrypted transport
func New(key ci.PrivKey) (*Transport, error) {
	id, err := peer.IDFromPrivateKey(key)
	if err != nil {
		return nil, err
	}
	t := &Transport{
		localPeer: id,
		privKey:   key,
	}

	identity, err := NewIdentity(key)
	if err != nil {
		return nil, err
	}
	t.identity = identity
	return t, nil
}

var _ sec.SecureTransport = &Transport{}

// SecureInbound runs the TLS handshake as a server.
// If p is empty, connections from any peer are accepted.
func (t *Transport) SecureInbound(ctx context.Context, insecure net.Conn, p peer.ID, muxers []string) (sec.SecureConn, error) {
	config, keyCh := t.identity.ConfigForPeer(p)
	// >>>>>> the above returned config consists of the tls nextprotocol fields that can be amended here.
	config.NextProtos = append(muxers, config.NextProtos...)
	fmt.Println(">>>>>> this is where TLS server is created and handshake carried out <<<<<<")
	cs, err := t.handshake(ctx, tls.Server(insecure, config), keyCh)
	if err != nil {
		addr, maErr := manet.FromNetAddr(insecure.RemoteAddr())
		if maErr == nil {
			canonicallog.LogPeerStatus(100, p, addr, "handshake_failure", "tls", "err", err.Error())
		}
		insecure.Close()
	}
	return cs, err
}

// SecureOutbound runs the TLS handshake as a client.
// Note that SecureOutbound will not return an error if the server doesn't
// accept the certificate. This is due to the fact that in TLS 1.3, the client
// sends its certificate and the ClientFinished in the same flight, and can send
// application data immediately afterwards.
// If the handshake fails, the server will close the connection. The client will
// notice this after 1 RTT when calling Read.
func (t *Transport) SecureOutbound(ctx context.Context, insecure net.Conn, p peer.ID, muxers []string) (sec.SecureConn, error) {
	config, keyCh := t.identity.ConfigForPeer(p)
	// >>>>>> the above returned config consists of the tls nextprotocol fields that can be amended here.
	config.NextProtos = append(muxers, config.NextProtos...)
	cs, err := t.handshake(ctx, tls.Client(insecure, config), keyCh)
	if err != nil {
		insecure.Close()
	}
	return cs, err
}

func (t *Transport) handshake(ctx context.Context, tlsConn *tls.Conn, keyCh <-chan ci.PubKey) (_sconn sec.SecureConn, err error) {
	defer func() {
		if rerr := recover(); rerr != nil {
			fmt.Fprintf(os.Stderr, "panic in TLS handshake: %s\n%s\n", rerr, debug.Stack())
			err = fmt.Errorf("panic in TLS handshake: %s", rerr)

		}
	}()

	// handshaking...
	fmt.Printf(">>> TLS handshaking <<< \n")
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	///
	ac := tlsConn.ConnectionState().NegotiatedProtocol
	fmt.Printf(">>>> TLS negotiated app protocol is %s", ac)

	// Should be ready by this point, don't block.
	var remotePubKey ci.PubKey
	select {
	case remotePubKey = <-keyCh:
	default:
	}
	if remotePubKey == nil {
		return nil, errors.New("go-libp2p tls BUG: expected remote pub key to be set")
	}

	return t.setupConn(tlsConn, remotePubKey)
}

func (t *Transport) setupConn(tlsConn *tls.Conn, remotePubKey ci.PubKey) (sec.SecureConn, error) {
	remotePeerID, err := peer.IDFromPublicKey(remotePubKey)
	if err != nil {
		return nil, err
	}

	nextProto := tlsConn.ConnectionState().NegotiatedProtocol
	if len(nextProto) > 0 && nextProto == "libp2p" {
		nextProto = ""
	}

	// here is where we can insert the NegotiatedProtocol data in te secureConn return value.
	// fmt.Println(" >>>>>> Adopted next proto: ", nextProto)

	return &conn{
		Conn:         tlsConn,
		localPeer:    t.localPeer,
		privKey:      t.privKey,
		remotePeer:   remotePeerID,
		remotePubKey: remotePubKey,
		earlyData:    nextProto,
	}, nil
}
