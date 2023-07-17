package autonatv2

import (
	"fmt"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/p2p/protocol/autonatv2/pb"
	"github.com/libp2p/go-msgio/pbio"
	ma "github.com/multiformats/go-multiaddr"
)

type Service struct {
	h host.Host
}

func (sr *Service) handleDialRequest(s network.Stream) {
	r := pbio.NewDelimitedReader(s, maxMsgSize)
	msg := &pb.Message{}
	if err := r.ReadMsg(msg); err != nil {
		fmt.Println(err)
		return
	}
	req := msg.GetDialRequest()
	if req == nil {
		return
	}
	addrs := make([]ma.Multiaddr, 0, len(req.GetAddrs()))
	for _, ab := range req.GetAddrs() {
		a, err := ma.NewMultiaddrBytes(ab)
		if err != nil {
			continue
		}
		addrs = append(addrs, a)
	}

	for _, a := range addrs {
		
	}

}
