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

	reader pbio.Reader
	buffer []byte

	closeOnce sync.Once
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
		read, readErr = r.readMessage(b)
	}
	return read, readErr
}

func (r *webRTCStreamReader) readMessage(b []byte) (int, error) {
	read := copy(b, r.buffer)
	r.buffer = r.buffer[read:]
	remaining := len(r.buffer)

	if remaining == 0 && !r.stream.stateHandler.AllowRead() {
		if closeErr := r.stream.getCloseErr(); closeErr != nil {
			log.Debugf("[2] stream closed: %v", closeErr)
			return read, closeErr
		}
		log.Debug("[2] stream has no more data to read")
		return read, io.EOF
	}

	if read > 0 {
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
			return read, io.ErrClosedPipe
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			// if the stream has been force closed or force reset
			// using SetReadDeadline, we check if closeErr was set.
			//
			// Deadlines don't close / reset streams, however if we call close
			// on a detached Pion datachannel when a Read call is in progress, it will not
			// cause the read call to return immediately. The Read only returns an io.EOF
			// when the remote closes the stream in response to the local response that is sent.
			//
			// To mitigate this issue, we set the closeErr var on the stream and set a ReadDeadline to a past time.
			// This causes any reads in progress to return immediately. FOr this, if a Read exists with ErrDeadlineExceeded,
			// we check if it was caused by closeErr being set.
			//
			// Please note: control and data packets are only read and processed from the underlying SCTP conn if
			// a read is called on the data channel. Please refer here:
			// https://github.com/pion/datachannel/blob/master/datachannel.go#L193-L200
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
		r.buffer = append(r.buffer, msg.GetMessage()...)
	}

	// process any flags on the message
	if msg.Flag != nil {
		r.stream.processIncomingFlag(msg.GetFlag())
	}
	return read, nil
}

func (r *webRTCStreamReader) readMessageFromDataChannel(msg *pb.Message) error {
	// TODO: remove this fn once the cyclic design is gone
	return r.reader.ReadMsg(msg)
}

func (r *webRTCStreamReader) SetReadDeadline(t time.Time) error {
	return r.stream.rwc.(*datachannel.DataChannel).SetReadDeadline(t)
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
