package libp2pwebrtc

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"time"

	pb "github.com/libp2p/go-libp2p/p2p/transport/webrtc/pb"
	"github.com/pion/datachannel"
	"google.golang.org/protobuf/proto"
)

func (s *webRTCStream) Write(b []byte) (int, error) {
	if !s.stateHandler.AllowWrite() {
		return 0, io.ErrClosedPipe
	}

	// Check if there is any message on the wire. This is used for control
	// messages only when the read side of the stream is closed
	if s.stateHandler.State() == stateReadClosed {
		s.readLoopOnce.Do(func() {
			go func() {
				// zero the read deadline, so read call only returns
				// when the underlying datachannel closes or there is
				// a message on the channel
				s.rwc.(*datachannel.DataChannel).SetReadDeadline(time.Time{})
				var msg pb.Message
				for {
					if s.stateHandler.Closed() {
						return
					}
					err := s.readMessageFromDataChannel(&msg)
					if err != nil {
						if errors.Is(err, io.EOF) {
							s.close(true, true)
						}
						return
					}
					if msg.Flag != nil {
						state, reset := s.stateHandler.HandleInboundFlag(msg.GetFlag())
						if state == stateClosed {
							log.Debug("closing: after handle inbound flag")
							s.close(reset, true)
						}
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
		end := len(b)
		if chunkSize < end {
			end = chunkSize
		}

		err := s.writeMessage(&pb.Message{Message: b[:end]})
		n += end
		b = b[end:]
		if err != nil {
			return n, err
		}
	}
	return n, err
}

func (s *webRTCStream) writeMessage(msg *pb.Message) error {
	var writeDeadlineTimer *time.Timer
	defer func() {
		if writeDeadlineTimer != nil {
			writeDeadlineTimer.Stop()
		}
	}()

	for {
		if !s.stateHandler.AllowWrite() {
			return io.ErrClosedPipe
		}
		// prepare waiting for writeAvailable signal
		// if write is blocked
		deadlineUpdated := s.writerDeadlineUpdated.Wait()
		writeAvailable := s.writeAvailable.Wait()

		writeDeadline, hasWriteDeadline := s.getWriteDeadline()
		if !hasWriteDeadline {
			writeDeadline = time.Unix(math.MaxInt64, 0)
		}
		if writeDeadlineTimer == nil {
			writeDeadlineTimer = time.NewTimer(time.Until(writeDeadline))
		} else {
			writeDeadlineTimer.Reset(time.Until(writeDeadline))
		}

		bufferedAmount := int(s.rwc.(*datachannel.DataChannel).BufferedAmount())
		addedBuffer := bufferedAmount + varintOverhead + proto.Size(msg)
		if addedBuffer > maxBufferedAmount {
			select {
			case <-writeDeadlineTimer.C:
				return os.ErrDeadlineExceeded
			case <-writeAvailable:
				err := s.writeMessageToWriter(msg)
				if err != nil {
					return err
				}
				return nil
			case <-s.ctx.Done():
				return io.ErrClosedPipe
			case <-deadlineUpdated:
			}
		} else {
			err := s.writeMessageToWriter(msg)
			if err != nil {
				return err
			}
			return nil
		}
	}
}

func (s *webRTCStream) writeMessageToWriter(msg *pb.Message) error {
	s.writerMux.Lock()
	defer s.writerMux.Unlock()
	return s.writer.WriteMsg(msg)
}

func (s *webRTCStream) SetWriteDeadline(t time.Time) error {
	s.writerDeadlineMux.Lock()
	defer s.writerDeadlineMux.Unlock()
	s.writerDeadline = t
	s.writerDeadlineUpdated.Signal()
	return nil
}

func (s *webRTCStream) getWriteDeadline() (time.Time, bool) {
	s.writerDeadlineMux.Lock()
	defer s.writerDeadlineMux.Unlock()
	return s.writerDeadline, !s.writerDeadline.IsZero()
}

func (s *webRTCStream) CloseWrite() error {
	if s.isClosed() {
		return nil
	}
	var err error
	s.closeOnce.Do(func() {
		err = s.writeMessage(&pb.Message{Flag: pb.Message_FIN.Enum()})
		if err != nil {
			log.Debug("could not write FIN message")
			err = fmt.Errorf("close stream for writing: %w", err)
			return
		}
		// if successfully written, process the outgoing flag
		state := s.stateHandler.CloseRead()
		// unblock and fail any ongoing writes
		s.writeAvailable.Signal()
		// check if closure required
		if state == stateClosed {
			s.close(false, true)
		}
	})
	return err
}
