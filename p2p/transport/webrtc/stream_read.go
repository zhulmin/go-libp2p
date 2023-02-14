package libp2pwebrtc

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	pb "github.com/libp2p/go-libp2p/p2p/transport/webrtc/pb"
	"github.com/libp2p/go-msgio/pbio"
	"github.com/pion/datachannel"
)

type webRTCStreamReader struct {
	stream *webRTCStream
	state  *webRTCStreamReaderState

	deadline    time.Time
	deadlineMux sync.Mutex

	closeOnce sync.Once
}

type webRTCStreamReaderState struct {
	sync.Mutex

	Reader pbio.Reader
	Buffer []byte

	stream *webRTCStream
}

// Read from the underlying datachannel. This also
// process sctp control messages such as DCEP, which is
// handled internally by pion, and stream closure which
// is signaled by `Read` on the datachannel returning
// io.EOF.
func (r *webRTCStreamReader) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}

	var (
		readErr error
		read    int
	)
	for read == 0 && readErr == nil {
		if r.stream.isClosed() {
			return 0, io.ErrClosedPipe
		}

		readDeadline, hasReadDeadline := r.getReadDeadline()
		if hasReadDeadline {
			// check if deadline exceeded
			if readDeadline.Before(time.Now()) {
				if err := r.stream.getCloseErr(); err != nil {
					log.Debugf("[1] deadline exceeded: closeErr: %v", err)
				} else {
					log.Debug("[1] deadline exceeded: no closeErr")
				}
				return 0, os.ErrDeadlineExceeded
			}
		}

		read, readErr = r.state.readMessage(b)
	}

	return read, readErr
}

func (r *webRTCStreamReaderState) readMessage(b []byte) (int, error) {
	r.Lock()
	defer r.Unlock()

	read := copy(b, r.Buffer)
	r.Buffer = r.Buffer[read:]
	remaining := len(r.Buffer)

	if remaining == 0 && !r.stream.stateHandler.AllowRead() {
		if closeErr := r.stream.getCloseErr(); closeErr != nil {
			log.Debugf("[2] stream closed: %v", closeErr)
			return read, closeErr
		}
		log.Debug("[2] stream empty")
		return read, io.EOF
	}

	if read > 0 {
		return read, nil
	}

	// read from datachannel
	var msg pb.Message
	err := r.Reader.ReadMsg(&msg)
	if err != nil {
		// This case occurs when the remote node goes away
		// without writing a FIN message
		if errors.Is(err, io.EOF) {
			r.stream.Reset()
			return read, io.ErrClosedPipe
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			// if the stream has been force closed or force reset
			// using SetReadDeadline, we check if closeErr was set.
			closeErr := r.stream.getCloseErr()
			log.Debugf("closing stream, checking error: %v closeErr: %v", err, closeErr)
			if closeErr != nil {
				return read, closeErr
			}
		}
		return read, err
	}

	// append incoming data to read buffer
	if r.stream.stateHandler.AllowRead() && msg.Message != nil {
		r.Buffer = append(r.Buffer, msg.GetMessage()...)
	}

	// process any flags on the message
	if msg.Flag != nil {
		r.stream.processIncomingFlag(msg.GetFlag())
	}
	return read, nil
}

func (r *webRTCStreamReaderState) readMessageFromDataChannel(msg *pb.Message) error {
	r.Lock()
	defer r.Unlock()
	return r.Reader.ReadMsg(msg)
}

func (r *webRTCStreamReader) SetReadDeadline(t time.Time) error {
	r.deadlineMux.Lock()
	defer r.deadlineMux.Unlock()
	r.deadline = t
	return r.stream.rwc.(*datachannel.DataChannel).SetReadDeadline(t)
}

func (r *webRTCStreamReader) getReadDeadline() (time.Time, bool) {
	r.deadlineMux.Lock()
	defer r.deadlineMux.Unlock()
	return r.deadline, !r.deadline.IsZero()
}

func (r *webRTCStreamReader) CloseRead() error {
	if r.stream.isClosed() {
		return nil
	}
	var err error
	r.closeOnce.Do(func() {
		err = r.stream.writer.writeMessageToWriter(&pb.Message{Flag: pb.Message_STOP_SENDING.Enum()})
		if err != nil {
			log.Debug("could not write STOP_SENDING message")
			err = fmt.Errorf("could not close stream for reading: %w", err)
			return
		}
		if r.stream.stateHandler.CloseRead() == stateClosed {
			r.stream.close(false, true)
		}
	})
	return err
}
