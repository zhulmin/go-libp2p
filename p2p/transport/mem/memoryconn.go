package memorytransport

import (
	"io"
	"net"
	"sync"
	"time"

	"runtime"

	"github.com/glycerine/rbuf"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr-net"
)

var InitialBufferSize = 5 * 1024 * 1024
var BufferGrowthFactor = 2

// GrowableRingBuffer wraps an rbuf.AtomicFixedSizeRingBuf with a mutex,
// allowing us to grow its capacity should it become filled.
type GrowableRingBuffer struct {
	mbuf *sync.RWMutex
	buf  *rbuf.AtomicFixedSizeRingBuf
}

func NewGrowableRingBuffer() *GrowableRingBuffer {
	return &GrowableRingBuffer{
		mbuf: new(sync.RWMutex),
		buf:  rbuf.NewAtomicFixedSizeRingBuf(InitialBufferSize),
	}
}

func (rb *GrowableRingBuffer) Readable() int {
	rb.mbuf.RLock()
	defer rb.mbuf.RUnlock()

	return rb.buf.Readable()
}

func (rb *GrowableRingBuffer) Write(bytes []byte) (int, error) {
	rb.mbuf.RLock()
	written, err := rb.buf.Write(bytes)
	if err != nil {
		// Buffer was short, grow
		rb.mbuf.RUnlock()
		rb.mbuf.Lock()
		defer rb.mbuf.Unlock()
		newSize := rb.buf.N * BufferGrowthFactor
		oldBuf := rb.buf
		rb.buf = rbuf.NewAtomicFixedSizeRingBuf(newSize)
		rb.buf.Adopt(oldBuf.Bytes(false))
		return rb.buf.Write(bytes[written:])
	}
	rb.mbuf.RUnlock()
	return written, nil
}

func (rb *GrowableRingBuffer) Read(out []byte) (int, error) {
	rb.mbuf.RLock()
	defer rb.mbuf.RUnlock()

	return rb.buf.Read(out)
}

// MemoryConn is a view into a pair of GrowableRingBuffers representing a fully
// duplexed connection. One is used for reading and the other is used for
// writing.
type MemoryConn struct {
	readbuf  *GrowableRingBuffer
	writebuf *GrowableRingBuffer
	parent   *MemoryConnPair
}

func NewMemoryConn(reader, writer *GrowableRingBuffer, parent *MemoryConnPair) *MemoryConn {
	return &MemoryConn{
		readbuf:  reader,
		writebuf: writer,
		parent:   parent,
	}
}

func (conn *MemoryConn) Close() error {
	return conn.parent.Close()
}

func (conn *MemoryConn) LocalAddr() net.Addr {
	return nil
}

func (conn *MemoryConn) RemoteAddr() net.Addr {
	return nil
}

func (conn *MemoryConn) LocalMultiaddr() multiaddr.Multiaddr {
	return nil
}

func (conn *MemoryConn) RemoteMultiaddr() multiaddr.Multiaddr {
	return nil
}

func (conn *MemoryConn) Read(out []byte) (int, error) {
	for {
		if conn.parent.closed {
			return 0, io.EOF
		}
		if conn.readbuf.Readable() > 0 {
			break
		}
		runtime.Gosched()
	}
	return conn.readbuf.Read(out)
}

func (conn *MemoryConn) Write(bytes []byte) (int, error) {
	return conn.writebuf.Write(bytes)
}

// TODO: implement?
func (conn *MemoryConn) SetDeadline(t time.Time) error {
	return nil
}

// TODO: implement?
func (conn *MemoryConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (conn *MemoryConn) SetWriteDeadline(t time.Time) error {
	return nil
}

var _ manet.Conn = (*MemoryConn)(nil)

// MemoryConnPair simulates a pipe
type MemoryConnPair struct {
	Listener  *MemoryConn
	Connector *MemoryConn
	closed    bool
}

func NewMemoryConnPair() *MemoryConnPair {
	a := NewGrowableRingBuffer()
	b := NewGrowableRingBuffer()
	pair := new(MemoryConnPair)
	pair.Listener = NewMemoryConn(a, b, pair)
	pair.Connector = NewMemoryConn(b, a, pair)
	pair.closed = false
	return pair
}

// TODO: use this actually
func (p *MemoryConnPair) Close() error {
	p.closed = true
	return nil
}
