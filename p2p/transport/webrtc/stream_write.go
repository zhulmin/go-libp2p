package libp2pwebrtc

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/p2p/transport/webrtc/internal"
	pb "github.com/libp2p/go-libp2p/p2p/transport/webrtc/pb"
	"github.com/libp2p/go-msgio/pbio"
	"github.com/pion/datachannel"
	"google.golang.org/protobuf/proto"
)

// Package pion detached data channel into a net.Conn
// and then a network.MuxedStream
type (
	webRTCStreamWriter struct {
		stream *webRTCStream
		writer pbio.Writer

		deadline int64

		deadlineUpdated internal.Signal
		writeAvailable  internal.Signal

		requestCh  chan *pb.Message
		responseCh chan webRTCStreamWriteResponse

		readLoopOnce sync.Once
		closeOnce    sync.Once
	}

	webRTCStreamWriteResponse struct {
		N     int
		Error error
	}
)

func (w *webRTCStreamWriter) Write(b []byte) (int, error) {
	if !w.stream.stateHandler.AllowWrite() {
		return 0, io.ErrClosedPipe
	}

	// Check if there is any message on the wire. This is used for control
	// messages only when the read side of the stream is closed
	if w.stream.stateHandler.State() == stateReadClosed {
		w.readLoopOnce.Do(func() {
			w.stream.wg.Add(1)
			go func() {
				defer w.stream.wg.Done()
				// zero the read deadline, so read call only returns
				// when the underlying datachannel closes or there is
				// a message on the channel
				w.stream.rwc.(*datachannel.DataChannel).SetReadDeadline(time.Time{})
				var msg pb.Message
				for {
					if w.stream.stateHandler.Closed() {
						return
					}
					err := w.stream.reader.ReadMsg(&msg)
					if err != nil {
						if errors.Is(err, io.EOF) {
							w.stream.close(true, true)
						}
						return
					}
					if msg.Flag != nil {
						w.stream.stateHandler.HandleInboundFlag(msg.GetFlag())
					}
				}
			}()
		})
	}

	const chunkSize = maxMessageSize - protoOverhead - varintOverhead

	var (
		err error
		n   int
	)

	for len(b) > 0 {
		end := internal.Min(chunkSize, len(b))

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
	case w.requestCh <- msg:
	case <-w.stream.ctx.Done():
		return 0, io.ErrClosedPipe
	}
	// get our final response back, effectively unblocking this writer
	// for a new writer
	select {
	case resp := <-w.responseCh:
		return resp.N, resp.Error
	case <-w.stream.ctx.Done():
		return 0, io.ErrClosedPipe
	}
}

// async writer in background
func (w *webRTCStreamWriter) runWriteLoop() {
	for {
		select {
		case msg := <-w.requestCh:
			n, err := w.write(msg)
			select {
			case w.responseCh <- webRTCStreamWriteResponse{N: n, Error: err}:
			case <-w.stream.ctx.Done():
				log.Debug("failed to send response: ctx closed")
			}
		case <-w.stream.ctx.Done():
			return
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
		if !w.stream.stateHandler.AllowWrite() {
			return 0, io.ErrClosedPipe
		}
		// prepare waiting for writeAvailable signal
		// if write is blocked
		deadlineUpdated := w.deadlineUpdated.Wait()
		writeAvailable := w.writeAvailable.Wait()

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
		w.stream.stateHandler.CloseRead()
		// unblock and fail any ongoing writes
		w.writeAvailable.Signal()
	})
	return err
}
