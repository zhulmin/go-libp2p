package libp2pwebtransport

import (
	"context"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"net"
)

type earlyDataHandler struct {
	earlyData []byte
	receive   func([]byte) error
}

var _ noise.EarlyDataHandler = &earlyDataHandler{}

func newEarlyDataSender(earlyData []byte) noise.EarlyDataHandler {
	return &earlyDataHandler{earlyData: earlyData}
}

func newEarlyDataReceiver(receive func([]byte) error) noise.EarlyDataHandler {
	return &earlyDataHandler{receive: receive}
}

func (e *earlyDataHandler) Send(context.Context, net.Conn, peer.ID) []byte {
	return e.earlyData
}

func (e *earlyDataHandler) Received(_ context.Context, _ net.Conn, b []byte) error {
	if e.receive == nil {
		return nil
	}
	return e.receive(b)
}
