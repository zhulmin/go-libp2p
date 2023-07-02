package libp2pwebrtc

import (
	"errors"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/p2p/transport/webrtc/pb"
)

// Read from the underlying datachannel.
// This also process SCTP control messages such as DCEP, which is handled internally by pion,
// and stream closure which is signaled by `Read` on the datachannel returning io.EOF.
func (s *stream) Read(b []byte) (int, error) {
	if s.closeErr != nil {
		return 0, s.closeErr
	}
	switch s.receiveState {
	case receiveStateDataRead:
		return 0, io.EOF
	case receiveStateReset:
		return 0, network.ErrReset
	}

	if s.nextMessage == nil {
		// load the next message
		var msg pb.Message
		if err := s.readMessageFromDataChannel(&msg); err != nil {
			if err == io.EOF {
				// if the channel was properly closed, return EOF
				if s.receiveState == receiveStateDataRead {
					return 0, io.EOF
				}
				// This case occurs when the remote node closes the stream without writing a FIN message
				// There's little we can do here
				return 0, errors.New("didn't receive final state for stream")
			}
			return 0, err
		}
		s.nextMessage = &msg
	}

	n := copy(b, s.nextMessage.Message)
	s.nextMessage.Message = s.nextMessage.Message[n:]
	if len(s.nextMessage.Message) > 0 {
		return n, nil
	}

	// process flags on the message after reading all the data
	s.processIncomingFlag(s.nextMessage.Flag)
	s.nextMessage = nil
	if s.closeErr != nil {
		return n, s.closeErr
	}
	switch s.receiveState {
	case receiveStateDataRead:
		return n, io.EOF
	case receiveStateReset:
		return n, network.ErrReset
	default:
		return n, nil
	}
}

func (s *stream) readMessageFromDataChannel(msg *pb.Message) error {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	return s.reader.ReadMsg(msg)
}

func (s *stream) SetReadDeadline(t time.Time) error { return s.dataChannel.SetReadDeadline(t) }

func (s *stream) CloseRead() error {
	s.receiveState = receiveStateReset
	if s.nextMessage != nil {
		s.processIncomingFlag(s.nextMessage.Flag)
		s.nextMessage = nil
	}
	err := s.sendControlMessage(&pb.Message{Flag: pb.Message_STOP_SENDING.Enum()})
	s.maybeDeclareStreamDone()
	return err
}
