package libp2pwebrtc

import (
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/p2p/transport/webrtc/internal/async"
	pb "github.com/libp2p/go-libp2p/p2p/transport/webrtc/pb"
	"github.com/libp2p/go-msgio/pbio"
	"github.com/pion/datachannel"
)

type (
	webRTCStreamReader struct {
		stream *webRTCStream

		state *async.MutexExec[*webRTCStreamReaderState]

		deadline async.MutexGetterSetter[time.Time]

		closeOnce sync.Once
	}

	webRTCStreamReaderState struct {
		Reader pbio.Reader
		Buffer []byte
	}
)

// Read from the underlying datachannel. This also
// process sctp control messages such as DCEP, which is
// handled internally by pion, and stream closure which
// is signaled by `Read` on the datachannel returning
// io.EOF.
func (r *webRTCStreamReader) Read(b []byte) (int, error) {
	var (
		readErr  error
		read     int
		finished bool
	)
	for !finished && readErr == nil {
		if r.stream.isClosed() {
			return 0, io.ErrClosedPipe
		}
		readDeadline, hasReadDeadline := r.getReadDeadline()
		if hasReadDeadline && readDeadline.Before(time.Now()) {
			log.Debug("[1] deadline exceeded: abort read")
			return 0, os.ErrDeadlineExceeded
		}

		readErr = r.state.Exec(func(state *webRTCStreamReaderState) error {
			read = copy(b, state.Buffer)
			state.Buffer = state.Buffer[read:]
			remaining := len(state.Buffer)

			if remaining == 0 && !r.stream.stateHandler.AllowRead() {
				log.Debugf("[2] stream closed or empty: %v", io.EOF)
				return io.EOF
			}

			if read > 0 || read == len(b) {
				finished = true
				return nil
			}

			// read from datachannel
			var msg pb.Message
			err := state.Reader.ReadMsg(&msg)
			if err != nil {
				// This case occurs when the remote node goes away
				// without writing a FIN message
				if errors.Is(err, io.EOF) {
					r.stream.Reset()
					return io.ErrClosedPipe
				}
				if errors.Is(err, os.ErrDeadlineExceeded) {
					if r.stream.stateHandler.Resetted() {
						return io.ErrClosedPipe
					} else {
						return io.EOF
					}
				}
				return err
			}

			// append incoming data to read buffer
			if r.stream.stateHandler.AllowRead() && msg.Message != nil {
				state.Buffer = append(state.Buffer, msg.GetMessage()...)
			}

			// process any flags on the message
			if msg.Flag != nil {
				r.stream.processIncomingFlag(msg.GetFlag())
			}
			return nil
		})
	}

	return read, readErr
}

func (r *webRTCStreamReader) SetReadDeadline(t time.Time) error {
	r.deadline.Set(t)
	return nil
}

func (r *webRTCStreamReader) getReadDeadline() (time.Time, bool) {
	return r.deadline.Get()
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

				if r.stream.stateHandler.Closed() {
					return
				}
				err := r.state.Exec(func(state *webRTCStreamReaderState) error {
					return state.Reader.ReadMsg(&msg)
				})
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
