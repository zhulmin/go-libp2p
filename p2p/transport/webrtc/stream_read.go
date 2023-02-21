package libp2pwebrtc

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	pb "github.com/libp2p/go-libp2p/p2p/transport/webrtc/pb"
	"github.com/pion/datachannel"
)

// Read from the underlying datachannel. This also
// process sctp control messages such as DCEP, which is
// handled internally by pion, and stream closure which
// is signaled by `Read` on the datachannel returning
// io.EOF.
func (s *webRTCStream) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}

	var (
		readErr error
		read    int
	)
	for read == 0 && readErr == nil {
		if s.isClosed() {
			return 0, io.ErrClosedPipe
		}
		read, readErr = s.readMessage(b)
	}
	return read, readErr
}

func (s *webRTCStream) readMessage(b []byte) (int, error) {
	read := copy(b, s.readBuffer)
	s.readBuffer = s.readBuffer[read:]
	remaining := len(s.readBuffer)

	if remaining == 0 && !s.stateHandler.AllowRead() {
		if closeErr := s.getCloseErr(); closeErr != nil {
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
	err := s.readMessageFromDataChannel(&msg)
	if err != nil {
		// This case occurs when the remote node goes away
		// without writing a FIN message
		if errors.Is(err, io.EOF) {
			s.Reset()
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
			closeErr := s.getCloseErr()
			log.Debugf("closing stream, checking error: %v closeErr: %v", err, closeErr)
			if closeErr != nil {
				return read, closeErr
			}
		}
		return read, err
	}

	// append incoming data to read readBuffer
	if s.stateHandler.AllowRead() && msg.Message != nil {
		s.readBuffer = append(s.readBuffer, msg.GetMessage()...)
	}

	// process any flags on the message
	if msg.Flag != nil {
		s.processIncomingFlag(msg.GetFlag())
	}
	return read, nil
}

func (s *webRTCStream) readMessageFromDataChannel(msg *pb.Message) error {
	s.readerMux.Lock()
	defer s.readerMux.Unlock()
	return s.reader.ReadMsg(msg)
}

func (s *webRTCStream) SetReadDeadline(t time.Time) error {
	return s.rwc.(*datachannel.DataChannel).SetReadDeadline(t)
}

func (s *webRTCStream) CloseRead() error {
	if s.isClosed() {
		return nil
	}
	var err error
	s.closeOnce.Do(func() {
		err = s.writeMessageToWriter(&pb.Message{Flag: pb.Message_STOP_SENDING.Enum()})
		if err != nil {
			log.Debug("could not write STOP_SENDING message")
			err = fmt.Errorf("could not close stream for reading: %w", err)
			return
		}
		if s.stateHandler.CloseRead() == stateClosed {
			s.close(false, true)
		}
	})
	return err
}
