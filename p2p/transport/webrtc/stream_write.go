package libp2pwebrtc

import (
	"errors"
	"os"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/p2p/transport/webrtc/pb"
)

var errWriteAfterClose = errors.New("write after close")

// If we have less space than minMessageSize, we don't put a new message on the data channel.
// Instead, we wait until more space opens up.
const minMessageSize = 1 << 10

func (s *stream) Write(b []byte) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if s.closeErr != nil {
		return 0, s.closeErr
	}
	switch s.sendState {
	case sendStateReset:
		return 0, network.ErrReset
	case sendStateDataSent:
		return 0, errWriteAfterClose
	}

	// Check if there is any message on the wire. This is used for control
	// messages only when the read side of the stream is closed
	if s.receiveState != receiveStateReceiving {
		s.readLoopOnce.Do(s.spawnControlMessageReader)
	}

	var writeDeadlineTimer *time.Timer
	defer func() {
		if writeDeadlineTimer != nil {
			writeDeadlineTimer.Stop()
		}
	}()

	var n int
	for len(b) > 0 {
		if s.closeErr != nil {
			return n, s.closeErr
		}
		switch s.sendState {
		case sendStateReset:
			return n, network.ErrReset
		case sendStateDataSent:
			return n, errWriteAfterClose
		}

		writeDeadline := s.writeDeadline
		// deadline deleted, stop and remove the timer
		if writeDeadline.IsZero() && writeDeadlineTimer != nil {
			writeDeadlineTimer.Stop()
			writeDeadlineTimer = nil
		}
		var writeDeadlineChan <-chan time.Time
		if !writeDeadline.IsZero() {
			if writeDeadlineTimer == nil {
				writeDeadlineTimer = time.NewTimer(time.Until(writeDeadline))
			} else {
				if !writeDeadlineTimer.Stop() {
					<-writeDeadlineTimer.C
				}
				writeDeadlineTimer.Reset(time.Until(writeDeadline))
			}
			writeDeadlineChan = writeDeadlineTimer.C
		}

		availableSpace := s.availableSendSpace()
		if availableSpace < minMessageSize {
			s.writeMu.Unlock()
			select {
			case <-s.writeAvailable:
			case <-writeDeadlineChan:
				s.writeMu.Lock()
				return n, os.ErrDeadlineExceeded
			case <-s.sendStateChanged:
			case <-s.writeDeadlineUpdated:
			}
			s.writeMu.Lock()
			continue
		}
		end := maxMessageSize
		if end > availableSpace {
			end = availableSpace
		}
		end -= protoOverhead + varintOverhead
		if end > len(b) {
			end = len(b)
		}
		msg := &pb.Message{Message: b[:end]}
		if err := s.writer.WriteMsg(msg); err != nil {
			return n, err
		}
		n += end
		b = b[end:]
	}
	return n, nil
}

// used for reading control messages while writing, in case the reader is closed,
// as to ensure we do still get control messages. This is important as according to the spec
// our data and control channels are intermixed on the same conn.
func (s *stream) spawnControlMessageReader() {
	if s.nextMessage != nil {
		s.processIncomingFlag(s.nextMessage.Flag)
		s.nextMessage = nil
	}
	go func() {
		// no deadline needed, Read will return once there's a new message, or an error occurred
		_ = s.dataChannel.SetReadDeadline(time.Time{})
		for {
			var msg pb.Message
			if err := s.readMessageFromDataChannel(&msg); err != nil {
				return
			}
			s.processIncomingFlag(msg.Flag)
		}
	}()
}

func (s *stream) SetWriteDeadline(t time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.writeDeadline = t
	select {
	case s.writeDeadlineUpdated <- struct{}{}:
	default:
	}
	return nil
}

func (s *stream) availableSendSpace() int {
	buffered := int(s.dataChannel.BufferedAmount())
	availableSpace := maxBufferedAmount - buffered
	if availableSpace < 0 { // this should never happen, but better check
		log.Errorw("data channel buffered more data than the maximum amount", "max", maxBufferedAmount, "buffered", buffered)
	}
	return availableSpace
}

const controlMsgSize = 100 // TODO: use actual message size

func (s *stream) sendControlMessage(msg *pb.Message) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	available := s.availableSendSpace()
	if controlMsgSize < available {
		return s.writer.WriteMsg(msg)
	}
	s.controlMsgQueue = append(s.controlMsgQueue, msg)
	return nil
}

func (s *stream) cancelWrite() error {
	if s.sendState != sendStateSending {
		return nil
	}
	s.sendState = sendStateReset
	select {
	case s.sendStateChanged <- struct{}{}:
	default:
	}
	if err := s.sendControlMessage(&pb.Message{Flag: pb.Message_RESET.Enum()}); err != nil {
		return err
	}
	s.maybeDeclareStreamDone()
	return nil
}

func (s *stream) CloseWrite() error {
	if s.sendState != sendStateSending {
		return nil
	}
	s.sendState = sendStateDataSent
	if err := s.sendControlMessage(&pb.Message{Flag: pb.Message_FIN.Enum()}); err != nil {
		return err
	}
	s.maybeDeclareStreamDone()
	return nil
}
