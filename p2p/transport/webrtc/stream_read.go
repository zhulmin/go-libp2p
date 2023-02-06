package libp2pwebrtc

import (
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/libp2p/go-libp2p/p2p/transport/webrtc/pb"
	"github.com/libp2p/go-msgio/pbio"
	"github.com/pion/datachannel"
)

type (
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
)

// Read from the underlying datachannel. This also
// process sctp control messages such as DCEP, which is
// handled internally by pion, and stream closure which
// is signaled by `Read` on the datachannel returning
// io.EOF.
func (r *webRTCStreamReader) Read(b []byte) (int, error) {
	// block until we have made our read request
	select {
	case r.requestCh <- b:
	case <-r.stream.ctx.Done():
		return 0, io.ErrClosedPipe
	}
	// get our final response back, effectively unblocking this reader
	// for a new reader
	select {
	case resp := <-r.responseCh:
		return resp.N, resp.Error
	case <-r.stream.ctx.Done():
		return 0, io.ErrClosedPipe
	}
}

// async reader in background
func (r webRTCStreamReader) runReadLoop() {
	for {
		select {
		case b := <-r.requestCh:
			n, err := r.read(b)
			select {
			case r.responseCh <- webRTCStreamReadResponse{N: n, Error: err}:
			case <-r.stream.ctx.Done():
				log.Debug("failed to send response: ctx closed")
			}
		case <-r.stream.ctx.Done():
			return
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

		if remaining == 0 && !r.stream.state.AllowRead() {
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
		if r.stream.state.AllowRead() && msg.Message != nil {
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

				if r.stream.state.Closed() {
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
