package autonatv2

import (
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
)

const DialProtocol = "/libp2p/autonat/2"

type Server struct {
	dialer network.Network
	host   host.Host
}

func (as *Server) Start() {
	as.host.SetStreamHandler(DialProtocol, as.handleDialRequest)
}

func (as *Server) Stop() {
	as.host.RemoveStreamHandler(DialProtocol)
}

func (as *Server) handleDialRequest(s network.Stream) {

}
