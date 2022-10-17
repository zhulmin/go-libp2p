package udpreuse

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go"
)

var quicConfig = &quic.Config{
	MaxIncomingStreams:         256,
	MaxIncomingUniStreams:      -1,             // disable unidirectional streams
	MaxStreamReceiveWindow:     10 * (1 << 20), // 10 MB
	MaxConnectionReceiveWindow: 15 * (1 << 20), // 15 MB
	RequireAddressValidation: func(net.Addr) bool {
		// TODO(#1535): require source address validation when under load
		return false
	},
	KeepAlivePeriod: 15 * time.Second,
	Versions:        []quic.VersionNumber{quic.VersionDraft29, quic.Version1},
}

type Listener interface {
	Accept(context.Context) (quic.Connection, error)
	Addr() net.Addr
	io.Closer
}

type protoConf struct {
	ln                  listener
	tlsConf             *tls.Config
	allowWindowIncrease func(conn quic.Connection, delta uint64) bool
}

type connListener struct {
	l     quic.Listener
	close chan struct{}

	mx        sync.Mutex
	protocols map[string]protoConf
}

func newConnListener(c Conn) (*connListener, error) {
	cl := &connListener{
		protocols: map[string]protoConf{},
		close:     make(chan struct{}),
	}
	tlsConf := &tls.Config{
		GetConfigForClient: func(info *tls.ClientHelloInfo) (*tls.Config, error) {
			cl.mx.Lock()
			defer cl.mx.Unlock()
			for _, proto := range info.SupportedProtos {
				if entry, ok := cl.protocols[proto]; ok {
					conf := entry.tlsConf
					if conf.GetConfigForClient != nil {
						return conf.GetConfigForClient(info)
					}
					return conf, nil
				}
			}
			panic("no supported proto")
			return nil, errors.New("no supported protocol found")
		},
	}
	quicConf := quicConfig.Clone()
	quicConf.AllowConnectionWindowIncrease = cl.allowWindowIncrease
	ln, err := quic.Listen(c, tlsConf, quicConf)
	if err != nil {
		return nil, err
	}
	cl.l = ln
	return cl, nil
}

func (l *connListener) allowWindowIncrease(conn quic.Connection, delta uint64) bool {
	l.mx.Lock()
	defer l.mx.Unlock()

	conf, ok := l.protocols[conn.ConnectionState().TLS.ConnectionState.NegotiatedProtocol]
	if !ok {
		return false
	}
	return conf.allowWindowIncrease(conn, delta)
}

func (l *connListener) Add(proto string, tlsConf *tls.Config, allowWindowIncrease func(conn quic.Connection, delta uint64) bool) (Listener, error) {
	l.mx.Lock()
	defer l.mx.Unlock()

	if _, ok := l.protocols[proto]; ok {
		return nil, fmt.Errorf("already listening for protocol %s", proto)
	}

	ln := newListener(l.l.Addr(), func() {
		l.mx.Lock()
		delete(l.protocols, proto)
		l.mx.Unlock()
	})
	l.protocols[proto] = protoConf{
		ln:                  *ln,
		tlsConf:             tlsConf,
		allowWindowIncrease: allowWindowIncrease,
	}
	return ln, nil
}

func (l *connListener) Run() error {
	// TODO: think about shutdown
	for {
		conn, err := l.l.Accept(context.Background())
		if err != nil {
			return err
		}
		proto := conn.ConnectionState().TLS.NegotiatedProtocol

		l.mx.Lock()
		ln, ok := l.protocols[proto]
		if !ok {
			l.mx.Unlock()
			return fmt.Errorf("negotiated unknown protocol: %s", proto)
		}
		ln.ln.add(conn)
		l.mx.Unlock()
	}
}

const queueLen = 16

type listener struct {
	queue  chan quic.Connection
	addr   net.Addr
	remove func()
}

var _ Listener = &listener{}

func newListener(addr net.Addr, remove func()) *listener {
	return &listener{
		queue:  make(chan quic.Connection, queueLen),
		remove: remove,
		addr:   addr,
	}
}

func (l *listener) add(c quic.Connection) {
	select {
	case l.queue <- c:
	default:
		c.CloseWithError(42, "queue full")
	}
}

func (l *listener) Accept(ctx context.Context) (quic.Connection, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case c, ok := <-l.queue:
		if !ok {
			return nil, errors.New("listener closed")
		}
		return c, nil
	}
}

func (l *listener) Addr() net.Addr {
	return l.addr
}

func (l *listener) Close() error {
	l.remove()
	close(l.queue)
	// drain the queue
	for conn := range l.queue {
		conn.CloseWithError(42, "closing")
	}
	return nil
}
