package libp2pwebrtc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	pb "github.com/libp2p/go-libp2p/p2p/transport/webrtc/pb"
	"github.com/libp2p/go-msgio/pbio"
	"github.com/pion/datachannel"
	"github.com/pion/webrtc/v3"
	"google.golang.org/protobuf/proto"
)

var _ network.MuxedStream = &webRTCStream{}

const (
	// maxMessageSize is limited to 16384 bytes in the SDP.
	maxMessageSize int = 16384
	// Pion SCTP association has an internal receive buffer of 1MB (roughly, 1MB per connection).
	// We can change this value in the SettingEngine before creating the peerconnection.
	// https://github.com/pion/webrtc/blob/v3.1.49/sctptransport.go#L341
	maxBufferedAmount int = 2 * maxMessageSize
	// bufferedAmountLowThreshold and maxBufferedAmount are bound
	// to a stream but congestion control is done on the whole
	// SCTP association. This means that a single stream can monopolize
	// the complete congestion control window (cwnd) if it does not
	// read stream data and it's remote continues to send. We can
	// add messages to the send buffer once there is space for 1 full
	// sized message.
	bufferedAmountLowThreshold uint64 = uint64(maxBufferedAmount) / 2

	// Proto overhead assumption is 5 bytes
	protoOverhead int = 5
	// Varint overhead is assumed to be 2 bytes. This is safe since
	// 1. This is only used and when writing message, and
	// 2. We only send messages in chunks of `maxMessageSize - varintOverhead`
	// which includes the data and the protobuf header. Since `maxMessageSize`
	// is less than or equal to 2 ^ 14, the varint will not be more than
	// 2 bytes in length.
	varintOverhead int = 2
)

// Package pion detached data channel into a net.Conn
// and then a network.MuxedStream
type (
	webRTCStream struct {
		webRTCStreamReader
		webRTCStreamWriter

		conn *connection
		id   uint16
		rwc  datachannel.ReadWriteCloser

		laddr net.Addr
		raddr net.Addr

		state *channelState

		readControlMessagesOnce sync.Once

		wg sync.WaitGroup

		ctx    context.Context
		cancel context.CancelFunc

		closeOnce sync.Once
	}

	webRTCStreamReader struct {
		stream *webRTCStream
		reader pbio.Reader

		deadline int64
		readBuf  []byte

		requestCh  chan []byte
		responseCh chan webRTCStreamReadResponse

		closeOnce sync.Once
	}

	webRTCStreamReadResponse struct {
		N     int
		Error error
	}

	webRTCStreamWriter struct {
		stream *webRTCStream
		writer pbio.Writer

		deadline int64

		deadlineUpdated signal
		writeAvailable  signal

		requestCh  chan *pb.Message
		responseCh chan webRTCStreamWriteResponse

		closeOnce sync.Once
	}

	webRTCStreamWriteResponse struct {
		N     int
		Error error
	}
)

func newStream(
	connection *connection,
	channel *webrtc.DataChannel,
	rwc datachannel.ReadWriteCloser,
	laddr, raddr net.Addr,
) *webRTCStream {
	ctx, cancel := context.WithCancel(context.Background())

	reader := bufio.NewReaderSize(rwc, maxMessageSize)

	result := &webRTCStream{
		webRTCStreamReader: webRTCStreamReader{
			reader:     pbio.NewDelimitedReader(reader, maxMessageSize),
			requestCh:  make(chan []byte),
			responseCh: make(chan webRTCStreamReadResponse),
		},
		webRTCStreamWriter: webRTCStreamWriter{
			writer:     pbio.NewDelimitedWriter(rwc),
			requestCh:  make(chan *pb.Message),
			responseCh: make(chan webRTCStreamWriteResponse),
		},

		conn: connection,
		id:   *channel.ID(),
		rwc:  rwc,

		laddr: laddr,
		raddr: raddr,

		state: newChannelState(),

		ctx:    ctx,
		cancel: cancel,
	}

	channel.SetBufferedAmountLowThreshold(bufferedAmountLowThreshold)
	channel.OnBufferedAmountLow(func() {
		result.writeAvailable.signal()
	})

	result.webRTCStreamReader.stream = result
	result.wg.Add(1)
	go func() {
		defer result.wg.Done()
		result.runReadLoop()
	}()

	result.webRTCStreamWriter.stream = result
	result.wg.Add(1)
	go func() {
		defer result.wg.Done()
		result.runWriteLoop()
	}()

	return result
}

func (s *webRTCStream) Close() error {
	return s.close(false, true)
}

func (s *webRTCStream) Reset() error {
	return s.close(true, true)
}

func (s *webRTCStream) LocalAddr() net.Addr {
	return s.laddr
}

func (s *webRTCStream) RemoteAddr() net.Addr {
	return s.raddr
}

func (s *webRTCStream) SetDeadline(t time.Time) error {
	return s.SetWriteDeadline(t)
}

func (s *webRTCStream) processIncomingFlag(flag pb.Message_Flag) {
	if s.isClosed() {
		return
	}
	state, stateUpdated := s.state.handleIncomingFlag(flag)
	if stateUpdated {
		if state.closed() {
			log.Debug("closing: received flag: %v", flag)
			err := s.close(flag == pb.Message_RESET, true)
			if err != nil {
				log.Debugf("failed to close (reset) stream: %v", err)
			}
		}
		if flag == pb.Message_FIN {
			// to ensure we keep reading flags, even after closing reader
			err := s.CloseRead()
			if err != nil {
				log.Debugf("failed to close Reader: %v", err)
			}
		}
	}
}

// this is used to force reset a stream
func (s *webRTCStream) close(isReset bool, notifyConnection bool) error {
	if s.isClosed() {
		return nil
	}

	var err error
	s.closeOnce.Do(func() {
		log.Debug("closing: reset: %v, notify: %v", isReset, notifyConnection)
		s.state.close()
		// force close reads
		s.SetReadDeadline(time.Now().Add(-100 * time.Millisecond))
		if isReset {
			// write the RESET message. The error is explicitly ignored
			// because we do not know if the remote is still connected
			s.writeMessage(&pb.Message{Flag: pb.Message_RESET.Enum()})
		} else {
			// write a FIN message for standard stream closure
			s.writeMessage(&pb.Message{Flag: pb.Message_FIN.Enum()})
		}
		s.wg.Wait()
		// close the context
		s.cancel()
		// close the channel. We do not care about the error message in
		// this case
		err = s.rwc.Close()
		if notifyConnection && s.conn != nil {
			s.conn.removeStream(s.id)
		}
	})

	return err
}

func (s *webRTCStream) isClosed() bool {
	select {
	case <-s.ctx.Done():
		return true
	default:
		return false
	}
}

// Read from the underlying datachannel. This also
// process sctp control messages such as DCEP, which is
// handled internally by pion, and stream closure which
// is signaled by `Read` on the datachannel returning
// io.EOF.
func (r *webRTCStreamReader) Read(b []byte) (int, error) {
	// block until we have made our read request
	select {
	case <-r.stream.ctx.Done():
		return 0, io.ErrClosedPipe
	case r.requestCh <- b:
	}
	// get our final response back, effectively unblocking this reader
	// for a new reader
	select {
	case <-r.stream.ctx.Done():
		return 0, io.ErrClosedPipe
	case resp := <-r.responseCh:
		return resp.N, resp.Error
	}
}

// async reader in background
func (r webRTCStreamReader) runReadLoop() {
	for {
		select {
		case <-r.stream.ctx.Done():
			return
		case b := <-r.requestCh:
			n, err := r.read(b)
			select {
			case r.responseCh <- webRTCStreamReadResponse{N: n, Error: err}:
			case <-r.stream.ctx.Done():
				log.Debug("failed to send response: ctx closed")
			}
		}
	}
}

func (r webRTCStreamReader) read(b []byte) (int, error) {
	var (
		readDeadlineEpoch = atomic.LoadInt64(&r.deadline)
		readDeadline      time.Time
	)
	if readDeadlineEpoch > 0 {
		readDeadline = time.UnixMicro(int64(readDeadlineEpoch))
	}

	for {
		if r.stream.isClosed() {
			return 0, io.ErrClosedPipe
		}
		if !readDeadline.IsZero() && readDeadline.Before(time.Now()) {
			log.Debug("[1] deadline exceeded: abort read")
			return 0, os.ErrDeadlineExceeded
		}

		read := copy(b, r.readBuf)
		r.readBuf = r.readBuf[read:]
		remaining := len(r.readBuf)

		if remaining == 0 && !r.stream.state.allowRead() {
			log.Debugf("[2] stream closed or empty: %v", io.EOF)
			return read, io.EOF
		}

		if read > 0 || read == len(b) {
			return read, nil
		}

		// read from datachannel
		var msg pb.Message
		err := r.reader.ReadMsg(&msg)
		if err != nil {
			// This case occurs when the remote node goes away
			// without writing a FIN message
			if errors.Is(err, io.EOF) {
				r.stream.Reset()
				return 0, io.ErrClosedPipe
			}
			return 0, err
		}

		// append incoming data to read buffer
		if r.stream.state.allowRead() && msg.Message != nil {
			r.readBuf = append(r.readBuf, msg.GetMessage()...)
		}

		// process any flags on the message
		if msg.Flag != nil {
			r.stream.processIncomingFlag(msg.GetFlag())
		}
	}
}

func (r *webRTCStreamReader) SetReadDeadline(t time.Time) error {
	atomic.StoreInt64(&r.deadline, t.UnixMicro())
	return nil
}

func (r *webRTCStreamReader) CloseRead() error {
	r.closeOnce.Do(func() {
		go func() {
			// zero the read deadline, so read call only returns
			// when the underlying datachannel closes or there is
			// a message on the channel
			r.stream.rwc.(*datachannel.DataChannel).SetReadDeadline(time.Time{})
			var msg pb.Message
			for {
				select {
				case <-r.stream.ctx.Done():
					return
				default:
				}

				if r.stream.state.closed() {
					return
				}
				err := r.reader.ReadMsg(&msg)
				if err != nil {
					if errors.Is(err, io.EOF) {
						r.stream.Reset()
					}
					return
				}
				if msg.Flag != nil {
					r.stream.processIncomingFlag(msg.GetFlag())
				}
			}
		}()
	})
	return nil
}

func (w *webRTCStreamWriter) Write(b []byte) (int, error) {
	state := w.stream.state.value()

	if !state.allowWrite() {
		return 0, io.ErrClosedPipe
	}

	const chunkSize = maxMessageSize - protoOverhead - varintOverhead

	var (
		err error
		n   int
	)

	for len(b) > 0 {
		end := min(chunkSize, len(b))

		written, err := w.writeMessage(&pb.Message{Message: b[:end]})
		n += written
		if err != nil {
			return n, err
		}
		b = b[end:]
	}
	return n, err
}

func (w *webRTCStreamWriter) writeMessage(msg *pb.Message) (int, error) {
	// block until we have made our write request
	select {
	case <-w.stream.ctx.Done():
		return 0, io.ErrClosedPipe
	case w.requestCh <- msg:
	}
	// get our final response back, effectively unblocking this writer
	// for a new writer
	select {
	case <-w.stream.ctx.Done():
		return 0, io.ErrClosedPipe
	case resp := <-w.responseCh:
		return resp.N, resp.Error
	}
}

// async writer in background
func (w *webRTCStreamWriter) runWriteLoop() {
	for {
		select {
		case <-w.stream.ctx.Done():
			return
		case msg := <-w.requestCh:
			n, err := w.write(msg)
			select {
			case w.responseCh <- webRTCStreamWriteResponse{N: n, Error: err}:
			case <-w.stream.ctx.Done():
				log.Debug("failed to send response: ctx closed")
			}
		}
	}
}

func (w *webRTCStreamWriter) write(msg *pb.Message) (int, error) {
	var (
		writeDeadlineEpoch = atomic.LoadInt64(&w.deadline)
		writeDeadline      time.Time
	)
	if writeDeadlineEpoch > 0 {
		writeDeadline = time.UnixMicro(int64(writeDeadlineEpoch))
	}

	// if the next message will add more data than we are willing to buffer,
	// block until we have sent enough bytes to reduce the amount of data buffered.
	timeout := make(chan struct{})
	var deadlineTimer *time.Timer

	for {
		if !w.stream.state.allowWrite() {
			return 0, io.ErrClosedPipe
		}
		// prepare waiting for writeAvailable signal
		// if write is blocked
		deadlineUpdated := w.deadlineUpdated.wait()
		writeAvailable := w.writeAvailable.wait()

		if !writeDeadline.IsZero() {
			// check if deadline exceeded
			if writeDeadline.Before(time.Now()) {
				return 0, os.ErrDeadlineExceeded
			}

			if deadlineTimer == nil {
				deadlineTimer = time.AfterFunc(time.Until(writeDeadline), func() { close(timeout) })
				defer deadlineTimer.Stop()
			}
			deadlineTimer.Reset(time.Until(writeDeadline))
		}

		bufferedAmount := int(w.stream.rwc.(*datachannel.DataChannel).BufferedAmount())
		addedBuffer := bufferedAmount + varintOverhead + proto.Size(msg)
		if addedBuffer > maxBufferedAmount {
			select {
			case <-timeout:
				return 0, os.ErrDeadlineExceeded
			case <-writeAvailable:
				err := w.writer.WriteMsg(msg)
				if err != nil {
					return 0, err
				}
				return int(len(msg.Message)), nil
			case <-w.stream.ctx.Done():
				return 0, io.ErrClosedPipe
			case <-deadlineUpdated:
			}
		} else {
			err := w.writer.WriteMsg(msg)
			if err != nil {
				return 0, err
			}
			return int(len(msg.Message)), nil
		}
	}
}

func (w *webRTCStreamWriter) SetWriteDeadline(t time.Time) error {
	atomic.StoreInt64(&w.deadline, t.UnixMicro())
	return nil
}

func (w *webRTCStreamWriter) CloseWrite() error {
	if w.stream.isClosed() {
		return nil
	}
	var err error
	w.closeOnce.Do(func() {
		_, err = w.writeMessage(&pb.Message{Flag: pb.Message_FIN.Enum()})
		if err != nil {
			log.Debug("could not write FIN message")
			err = fmt.Errorf("close stream for writing: %w", err)
			return
		}
		// if successfully written, process the outgoing flag
		state, stateUpdated := w.stream.state.processOutgoingFlag(pb.Message_FIN)
		// unblock and fail any ongoing writes
		w.writeAvailable.signal()
		// check if closure required
		if stateUpdated && state.closed() {
			w.stream.close(false, true)
		}
	})
	return err
}

type signal struct {
	sync.Mutex
	c chan struct{}
}

func (s *signal) wait() <-chan struct{} {
	s.Lock()
	defer s.Unlock()
	return s.c
}

func (s *signal) signal() {
	s.Lock()
	c := s.c
	s.c = make(chan struct{})
	s.Unlock()
	close(c)
}
