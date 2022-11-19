package quicreuse

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/lucas-clemente/quic-go"
	ma "github.com/multiformats/go-multiaddr"
)

var quicListen = quic.Listen // so we can mock it in tests

type Listener interface {
	Accept(context.Context) (quic.Connection, error)
	Addr() net.Addr
	Multiaddrs() []ma.Multiaddr
	io.Closer
}

type protoConf struct {
	ln                  *listener
	tlsConf             *tls.Config
	allowWindowIncrease func(conn quic.Connection, delta uint64) bool
}

type connListener struct {
	l       quic.Listener
	conn    pConn
	running chan struct{}
	addrs   []ma.Multiaddr

	mx        sync.Mutex
	protocols map[string]protoConf
}

func newConnListener(c pConn, quicConfig *quic.Config, enableDraft29 bool) (*connListener, error) {
	localMultiaddrs := make([]ma.Multiaddr, 0, 2)
	a, err := ToQuicMultiaddr(c.LocalAddr(), quic.Version1)
	if err != nil {
		return nil, err
	}
	localMultiaddrs = append(localMultiaddrs, a)
	if enableDraft29 {
		a, err := ToQuicMultiaddr(c.LocalAddr(), quic.VersionDraft29)
		if err != nil {
			return nil, err
		}
		localMultiaddrs = append(localMultiaddrs, a)
	}
	cl := &connListener{
		protocols: map[string]protoConf{},
		running:   make(chan struct{}),
		conn:      c,
		addrs:     localMultiaddrs,
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
			return nil, errors.New("no supported protocol found")
		},
	}
	quicConf := quicConfig.Clone()
	quicConf.AllowConnectionWindowIncrease = cl.allowWindowIncrease
	ln, err := quicListen(c, tlsConf, quicConf)
	if err != nil {
		return nil, err
	}
	cl.l = ln
	go cl.Run() // This go routine shuts down once the underlying quic.Listener is closed (or returns an error).
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

func (l *connListener) Add(tlsConf *tls.Config, allowWindowIncrease func(conn quic.Connection, delta uint64) bool, onRemove func()) (Listener, error) {
	l.mx.Lock()
	defer l.mx.Unlock()

	if len(tlsConf.NextProtos) == 0 {
		return nil, errors.New("no ALPN found in tls.Config")
	}

	for _, proto := range tlsConf.NextProtos {
		if _, ok := l.protocols[proto]; ok {
			return nil, fmt.Errorf("already listening for protocol %s", proto)
		}
	}

	ln := newSingleListener(l.l.Addr(), l.addrs, func() {
		l.mx.Lock()
		for _, proto := range tlsConf.NextProtos {
			delete(l.protocols, proto)
		}
		l.mx.Unlock()
		onRemove()
	})
	for _, proto := range tlsConf.NextProtos {
		l.protocols[proto] = protoConf{
			ln:                  ln,
			tlsConf:             tlsConf,
			allowWindowIncrease: allowWindowIncrease,
		}
	}
	return ln, nil
}

func (l *connListener) Run() error {
	defer close(l.running)
	defer l.conn.DecreaseCount()
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

func (l *connListener) Close() error {
	err := l.l.Close()
	<-l.running // wait for Run to return
	return err
}

const queueLen = 16

// A listener for a single ALPN protocol (set).
type listener struct {
	queue     chan quic.Connection
	addr      net.Addr
	addrs     []ma.Multiaddr
	remove    func()
	closeOnce sync.Once
}

var _ Listener = &listener{}

func newSingleListener(addr net.Addr, addrs []ma.Multiaddr, remove func()) *listener {
	return &listener{
		queue:  make(chan quic.Connection, queueLen),
		remove: remove,
		addr:   addr,
		addrs:  addrs,
	}
}

func (l *listener) add(c quic.Connection) {
	select {
	case l.queue <- c:
	default:
		c.CloseWithError(1, "queue full")
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

func (l *listener) Multiaddrs() []ma.Multiaddr {
	return l.addrs
}

func (l *listener) Close() error {
	l.closeOnce.Do(func() {
		l.remove()
		close(l.queue)
		// drain the queue
		for conn := range l.queue {
			conn.CloseWithError(1, "closing")
		}
	})
	return nil
}
