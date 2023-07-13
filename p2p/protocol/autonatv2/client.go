package autonatv2

import (
	"context"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/autonatv2/pb"
	"github.com/libp2p/go-msgio/pbio"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-varint"
	"golang.org/x/exp/rand"
)

//go:generate protoc --go_out=. --go_opt=Mpb/autonat.proto=./pb pb/autonat.proto

type Client struct {
	h          host.Host
	dialCharge []byte
}

func (c *Client) DialRequest(addrs []ma.Multiaddr) (DialResponse, error) {
	peers := c.validPeers()
	for _, p := range peers {
		if resp, err := c.tryPeer(p, addrs); err == nil {
			return resp, nil
		}
	}
	return DialResponse{}, nil
}

func (c *Client) validPeers() []peer.ID {
	peers := c.h.Peerstore().Peers()
	idx := 0
	for _, p := range c.h.Peerstore().Peers() {
		if proto, err := c.h.Peerstore().SupportsProtocols(p, protocol); len(proto) == 0 || err != nil {
			continue
		}
		peers[idx] = p
		idx++
	}
	peers = peers[:idx]
	rand.Shuffle(len(peers), func(i, j int) { peers[i], peers[j] = peers[j], peers[i] })
	return peers
}

func (c *Client) tryPeer(p peer.ID, addrs []ma.Multiaddr) (DialResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := c.h.NewStream(ctx, p, protocol)
	s.SetDeadline(time.Now().Add(1 * time.Minute))
	if err != nil {
		return DialResponse{}, err
	}
	nonce := rand.Uint64()
	msg := &pb.Message{
		Msg: &pb.Message_DialRequest{
			DialRequest: &pb.DialRequest{
				Addrs: make([][]byte, len(addrs)),
				Nonce: nonce,
			},
		},
	}
	r := pbio.NewDelimitedReader(s, maxMsgSize)
	w := pbio.NewDelimitedWriter(s)
	if err := w.WriteMsg(msg); err != nil {
		return DialResponse{}, err
	}

	msg.Reset()
	if err := r.ReadMsg(msg); err != nil {
		return DialResponse{}, err
	}

	if msg.GetDialDataRequest() != nil {
		nb := msg.GetDialDataRequest().GetNumBytes()
		s.Write(varint.ToUvarint(nb))
		for rem := int(nb); rem > 0; {
			if rem > len(c.dialCharge) {
				n, err := s.Write(c.dialCharge)
				if err != nil {
					return DialResponse{}, err
				}
				rem -= n
			} else {
				n, err := s.Write(c.dialCharge[:rem])
				if err != nil {
					return DialResponse{}, err
				}
				rem -= n
			}
		}
		msg.Reset()
		if err := r.ReadMsg(msg); err != nil {
			return DialResponse{}, err
		}
	}

}

type DialResponse struct {
	Status  pb.DialResponse_ResponseStatus
	Results []DialResult
}

type DialResult struct {
	ExternalAddr ma.Multiaddr
	LocalAddr    ma.Multiaddr
	Status       pb.DialResponse_DialStatus
}
