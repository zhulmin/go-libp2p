package pipetransport

import (
	"fmt"
	"net"

	streammux "github.com/libp2p/go-stream-muxer"
)

var ErrHalfClosed = fmt.Errorf("tried to write to stream that was half closed")
var ErrAlreadyHalfClosed = fmt.Errorf("tried to half close stream that was already half closed")

type PipeStream struct {
	net.Conn

	companion net.Conn
}

func NewPipeStream(conn net.Conn, companion net.Conn) *PipeStream {
	return &PipeStream{
		Conn:      conn,
		companion: companion,
	}
}

func (s *PipeStream) Reset() error {
	err1 := s.Conn.Close()
	err2 := s.companion.Close()
	if err1 != nil {
		return err1
	}
	if err2 != nil {
		return err2
	}
	return nil
}

var _ streammux.Stream = (*PipeStream)(nil)
