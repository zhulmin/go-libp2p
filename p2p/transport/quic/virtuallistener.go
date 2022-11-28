package libp2pquic

import (
	"errors"
	"sync"

	tpt "github.com/libp2p/go-libp2p/core/transport"
	"github.com/libp2p/go-libp2p/p2p/transport/quicreuse"
	"github.com/lucas-clemente/quic-go"
	ma "github.com/multiformats/go-multiaddr"
)

const acceptBufferPerVersion = 4

// virtualListener is a listener that exposes a single multiaddr but uses another listener under the hood
type virtualListener struct {
	*listener
	udpAddr       string
	version       quic.VersionNumber
	t             *transport
	acceptRunnner *acceptLoopRunner
	acceptChan    chan acceptVal
}

var _ tpt.Listener = &virtualListener{}

func (l *virtualListener) Multiaddr() ma.Multiaddr {
	return l.listener.localMultiaddrs[l.version]
}

func (l *virtualListener) Close() error {
	l.t.listenersMu.Lock()
	defer l.t.listenersMu.Unlock()
	l.acceptRunnner.rmAcceptForVersion(l.version)

	var err error
	listeners := l.t.listeners[l.udpAddr]
	if len(listeners) == 1 {
		// This is the last virtual listener here, so we can close the underlying listener
		err = l.listener.Close()
		delete(l.t.listeners, l.udpAddr)
	} else {
		for i := 0; i < len(listeners); i++ {
			// Swap remove
			if l == listeners[i] {
				listeners[i] = listeners[len(listeners)-1]
				listeners = listeners[0 : len(listeners)-1]
				l.t.listeners[l.udpAddr] = listeners
				break
			}
		}
	}

	return err
}

func (l *virtualListener) Accept() (tpt.CapableConn, error) {
	v, ok := <-l.acceptChan
	if !ok {
		return nil, errors.New("listener closed")
	}

	return v.conn, v.err
}

type acceptVal struct {
	conn tpt.CapableConn
	err  error
}

type acceptLoopRunner struct {
	muxerMu sync.Mutex
	muxer   map[quic.VersionNumber]chan acceptVal
}

func (r *acceptLoopRunner) acceptForVersion(v quic.VersionNumber) chan acceptVal {
	r.muxerMu.Lock()
	defer r.muxerMu.Unlock()

	ch := make(chan acceptVal, acceptBufferPerVersion)

	if _, ok := r.muxer[v]; ok {
		panic("unexpected chan already found in accept muxer")
	}

	r.muxer[v] = ch
	return ch
}

func (r *acceptLoopRunner) rmAcceptForVersion(v quic.VersionNumber) {
	r.muxerMu.Lock()
	defer r.muxerMu.Unlock()

	ch, ok := r.muxer[v]
	if !ok {
		panic("unexpected chan already found in accept muxer")
	}
	ch <- acceptVal{err: errors.New("listener Accept closed")}
	delete(r.muxer, v)
}

func (r *acceptLoopRunner) sendErrAndClose(err error) {
	r.muxerMu.Lock()
	defer r.muxerMu.Unlock()
	for k, ch := range r.muxer {
		select {
		case ch <- acceptVal{err: err}:
		default:
		}
		delete(r.muxer, k)
		close(ch)
	}
}

func (r *acceptLoopRunner) run(l *listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			r.sendErrAndClose(err)
			return err
		}

		_, version, err := quicreuse.FromQuicMultiaddr(conn.RemoteMultiaddr())
		if err != nil {
			r.sendErrAndClose(err)
			return err
		}

		r.muxerMu.Lock()
		ch, ok := r.muxer[version]
		r.muxerMu.Unlock()

		if !ok {
			// Nothing to handle this connection version. Close it
			conn.Close()
			continue
		}

		// Non blocking
		select {
		case ch <- acceptVal{conn: conn}:
		default:
			// We dropped the connection, close it
			conn.Close()
			continue
		}
	}
}
