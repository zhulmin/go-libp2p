package noise

import (
	"context"
	"net"

	"github.com/libp2p/go-libp2p/core/canonicallog"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/sec"
	manet "github.com/multiformats/go-multiaddr/net"
)

type SessionOption = func(*SessionTransport) error

// Prologue sets a prologue for the Noise session.
// The handshake will only complete successfully if both parties set the same prologue.
// See https://noiseprotocol.org/noise.html#prologue for details.
func Prologue(prologue []byte) SessionOption {
	return func(s *SessionTransport) error {
		s.prologue = prologue
		return nil
	}
}

// EarlyDataHandler defines what the application payload is for either the first
// (if initiator) or second (if responder) handshake message. And defines the
// logic for handling the other side's early data. Note the early data in the
// first handshake message is **unencrypted**, but will be retroactively
// authenticated if the handshake completes.
type EarlyDataHandler interface {
	// Send for the initiator is called for the client before sending the first
	// handshake message. Defines the application payload for the first message.
	// This payload is sent **unencrypted**.
	// Send for the responder is called before sending the second handshake message. This is encrypted.
	Send(context.Context, net.Conn, peer.ID) []byte
	// Received for the initiator is called when the second handshake message
	// from the responder is received.
	// Received for the responder is called when the first handshake message
	// from the initiator is received.
	Received(context.Context, net.Conn, []byte) error
}

// EarlyData sets the `EarlyDataHandler` for the initiator and responder roles.
// See `EarlyDataHandler` for more details. Note: an initiator's early data will
// be sent **unencrypted** in the first handshake message.
func EarlyData(initiator, responder EarlyDataHandler) SessionOption {
	return func(s *SessionTransport) error {
		s.initiatorEarlyDataHandler = initiator
		s.responderEarlyDataHandler = responder
		return nil
	}
}

var _ sec.SecureTransport = &SessionTransport{}

// SessionTransport can be used
// to provide per-connection options
type SessionTransport struct {
	t *Transport
	// options
	prologue []byte

	initiatorEarlyDataHandler, responderEarlyDataHandler EarlyDataHandler
}

// SecureInbound runs the Noise handshake as the responder.
// If p is empty, connections from any peer are accepted.
func (i *SessionTransport) SecureInbound(ctx context.Context, insecure net.Conn, p peer.ID) (sec.SecureConn, error) {
	c, err := newSecureSession(i.t, ctx, insecure, p, i.prologue, i.initiatorEarlyDataHandler, i.responderEarlyDataHandler, false)
	if err != nil {
		addr, maErr := manet.FromNetAddr(insecure.RemoteAddr())
		if maErr == nil {
			canonicallog.LogPeerStatus(100, p, addr, "handshake_failure", "noise", "err", err.Error())
		}
	}
	return c, err
}

// SecureOutbound runs the Noise handshake as the initiator.
func (i *SessionTransport) SecureOutbound(ctx context.Context, insecure net.Conn, p peer.ID) (sec.SecureConn, error) {
	return newSecureSession(i.t, ctx, insecure, p, i.prologue, i.initiatorEarlyDataHandler, i.responderEarlyDataHandler, true)
}
