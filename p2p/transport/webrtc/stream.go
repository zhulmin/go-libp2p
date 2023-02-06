package libp2pwebrtc

import (
	"bufio"
	"context"
	"net"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/p2p/transport/webrtc/internal"
	pb "github.com/libp2p/go-libp2p/p2p/transport/webrtc/pb"
	"github.com/libp2p/go-msgio/pbio"
	"github.com/pion/datachannel"
	"github.com/pion/webrtc/v3"
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

		state *internal.ChannelState

		wg sync.WaitGroup

		ctx    context.Context
		cancel context.CancelFunc

		closeOnce sync.Once
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

		state: internal.NewChannelState(),

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
	state, stateUpdated := s.state.HandleIncomingFlag(flag)
	if stateUpdated {
		if state.Closed() {
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
		s.state.Close()
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
