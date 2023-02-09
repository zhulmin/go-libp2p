package libp2pwebrtc

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/p2p/transport/webrtc/internal"
	"github.com/libp2p/go-libp2p/p2p/transport/webrtc/internal/async"
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

		writer   *async.MutexExec[pbio.Writer]
		deadline async.MutexGetterSetter[time.Time]

		deadlineUpdated async.CondVar
		writeAvailable  async.CondVar

		readLoopOnce sync.Once
		closeOnce    sync.Once
	}
)

func (w *webRTCStreamWriter) Write(b []byte) (int, error) {
	w.stream.wg.Add(1)
	defer w.stream.wg.Done()

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
					err := w.stream.reader.state.Exec(func(state *webRTCStreamReaderState) error {
						return state.Reader.ReadMsg(&msg)
					})
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
	w.stream.wg.Add(1)
	defer w.stream.wg.Done()

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

		writeDeadline, hasWriteDeadline := w.getWriteDeadline()
		if hasWriteDeadline {
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
				err := w.writer.Exec(func(writer pbio.Writer) error {
					return writer.WriteMsg(msg)
				})
				if err != nil {
					return 0, err
				}
				return int(len(msg.Message)), nil
			case <-w.stream.ctx.Done():
				return 0, io.ErrClosedPipe
			case <-deadlineUpdated:
			}
		} else {
			err := w.writer.Exec(func(writer pbio.Writer) error {
				return writer.WriteMsg(msg)
			})
			if err != nil {
				return 0, err
			}
			return int(len(msg.Message)), nil
		}
	}
}

func (w *webRTCStreamWriter) SetWriteDeadline(t time.Time) error {
	w.deadline.SetWithCond(t, &w.deadlineUpdated)
	return nil
}

func (w *webRTCStreamWriter) getWriteDeadline() (time.Time, bool) {
	return w.deadline.Get()
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
