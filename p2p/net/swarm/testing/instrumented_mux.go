package testing

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"unicode"

	"github.com/libp2p/go-libp2p-core/network"

	logging "github.com/ipfs/go-log/v2"
)

var log = logging.Logger("insmux")

type InstrumentedMuxedStream struct {
	network.MuxedStream
	rootMultiplexer *InstrumetedMultiplexer
}

func (s *InstrumentedMuxedStream) Write(b []byte) (n int, err error) {
	return s.MuxedStream.Write(b)
}

func formatBytes(b []byte) string {
	var out []string
	lastRange := 0
	for i, byt := range b {
		if byt > unicode.MaxASCII || byt < 0x21 {
			out = append(out, string(b[lastRange:i]))
			lastRange = i + 1
			out = append(out, fmt.Sprintf("\\x%02x", byt))
		}
	}
	if lastRange != len(b) {
		out = append(out, string(b[lastRange:]))
	}
	return strings.Join(out, "")
}

func (s *InstrumentedMuxedStream) Read(b []byte) (n int, err error) {
	n, err = s.MuxedStream.Read(b)
	if n == 0 {
		return n, err
	}

	bytesRead := b[:n]
	log.Infof("%s<-: %s", s.rootMultiplexer.Name, formatBytes(bytesRead))

	foundOne := false
	s.rootMultiplexer.expectMu.RLock()
	defer func() {
		if !foundOne {
			// If we found a match, we unlock this already
			s.rootMultiplexer.expectMu.RUnlock()
		}

	}()

	// Look through our expect to read checks to see if we need to close any chans.
	for i := 0; i < len(s.rootMultiplexer.expectReadChecks); i++ {
		check := s.rootMultiplexer.expectReadChecks[i]
		if bytes.Contains(bytesRead, check) {
			if !foundOne {
				// If we have our first match, we need to release the read lock and get the write lock.
				// We'll keep the write lock until we return after we've found at least one match.
				foundOne = true
				s.rootMultiplexer.expectMu.RUnlock()

				s.rootMultiplexer.expectMu.Lock()
				defer s.rootMultiplexer.expectMu.Unlock()
			}

			if !bytes.Equal(s.rootMultiplexer.expectReadChecks[i], check) {
				// Some other goroutine beat us to it.
				return n, err
			}

			ch := s.rootMultiplexer.expectReadChns[i]

			if len(s.rootMultiplexer.expectReadChecks) == 1 {
				s.rootMultiplexer.expectReadChecks = nil
				s.rootMultiplexer.expectReadChns = nil
			} else {
				s.rootMultiplexer.expectReadChecks[i] = s.rootMultiplexer.expectReadChecks[len(s.rootMultiplexer.expectReadChecks)-1]
				s.rootMultiplexer.expectReadChecks = s.rootMultiplexer.expectReadChecks[:len(s.rootMultiplexer.expectReadChecks)-1]

				s.rootMultiplexer.expectReadChns[i] = s.rootMultiplexer.expectReadChns[len(s.rootMultiplexer.expectReadChns)-1]
				s.rootMultiplexer.expectReadChns = s.rootMultiplexer.expectReadChns[:len(s.rootMultiplexer.expectReadChns)-1]
			}

			close(ch)

			// Go back one so that we check the new check at this index
			i = i - 1
			continue
		}
	}

	return n, err
}

type InstrumentedMuxedConn struct {
	network.MuxedConn
	rootMultiplexer *InstrumetedMultiplexer
}

func (c *InstrumentedMuxedConn) OpenStream(ctx context.Context) (network.MuxedStream, error) {
	s, err := c.MuxedConn.OpenStream(ctx)
	if err != nil {
		return nil, err
	}

	return &InstrumentedMuxedStream{s, c.rootMultiplexer}, nil
}

func (c *InstrumentedMuxedConn) AcceptStream() (network.MuxedStream, error) {
	s, err := c.MuxedConn.AcceptStream()
	if err != nil {
		return nil, err
	}

	return &InstrumentedMuxedStream{s, c.rootMultiplexer}, nil
}

type InstrumetedMultiplexer struct {
	network.Multiplexer
	// a human readable name for the logs to identify this host
	Name             string
	expectMu         sync.RWMutex
	expectReadChecks [][]byte
	expectReadChns   []chan struct{}
}

func (m *InstrumetedMultiplexer) NewConn(c net.Conn, isServer bool, scope network.PeerScope) (network.MuxedConn, error) {
	mc, err := m.Multiplexer.NewConn(c, isServer, scope)
	if err != nil {
		return nil, err
	}

	return &InstrumentedMuxedConn{mc, m}, nil
}

// ExpectReadBytes returns a chan that is closed when the given bytes are read
// from the stream. Note that if the bytes are split in between multiple read
// calls this will fail.
func (m *InstrumetedMultiplexer) ExpectReadBytes(b []byte) <-chan struct{} {
	m.expectMu.Lock()
	defer m.expectMu.Unlock()

	ch := make(chan struct{})
	m.expectReadChecks = append(m.expectReadChecks, b)
	m.expectReadChns = append(m.expectReadChns, ch)

	return ch
}

func (m *InstrumetedMultiplexer) SwarmOptions() []Option {
	// Disable QUIC so we use this multiplexer instead of the quic native one.
	return []Option{Multiplexer(m), OptDisableQUIC}
}
