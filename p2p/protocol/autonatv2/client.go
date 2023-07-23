package autonatv2

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/autonatv2/pb"
	"github.com/libp2p/go-msgio/pbio"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-varint"
	"golang.org/x/exp/rand"
)

//go:generate protoc --go_out=. --go_opt=Mpb/autonat.proto=./pb pb/autonat.proto

var (
	ErrNoValidPeers = errors.New("autonat v2: No valid peers")
)

type Client struct {
	h          host.Host
	dialCharge []byte

	mu            sync.Mutex
	attemptQueues map[uint64]chan attempt
}

func (c *Client) DialRequest(addrs []ma.Multiaddr) (DialResponse, error) {
	peers := c.validPeers()
	if len(peers) == 0 {
		return DialResponse{}, ErrNoValidPeers
	}
	return c.tryPeer(peers[0], addrs)
}

func (c *Client) validPeers() []peer.ID {
	peers := c.h.Peerstore().Peers()
	idx := 0
	for _, p := range c.h.Peerstore().Peers() {
		if proto, err := c.h.Peerstore().SupportsProtocols(p, dialProtocol); len(proto) == 0 || err != nil {
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
	c.h.SetStreamHandler(attemptProtocol, c.handleDialAttempt)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	s, err := c.h.NewStream(ctx, p, dialProtocol)
	if err != nil {
		return DialResponse{}, err
	}
	s.SetDeadline(time.Now().Add(1 * time.Minute))
	if err != nil {
		return DialResponse{}, err
	}
	nonce := rand.Uint64()
	ch := make(chan attempt, 1)
	c.mu.Lock()
	c.attemptQueues[nonce] = ch
	c.mu.Unlock()

	addrsb := make([][]byte, len(addrs))
	for i, a := range addrs {
		addrsb[i] = a.Bytes()
	}
	msg := &pb.Message{
		Msg: &pb.Message_DialRequest{
			DialRequest: &pb.DialRequest{
				Addrs: addrsb,
				Nonce: nonce,
			},
		},
	}
	w := pbio.NewDelimitedWriter(s)
	if err := w.WriteMsg(msg); err != nil {
		return DialResponse{}, err
	}

	msg.Reset()
	r := pbio.NewDelimitedReader(s, maxMsgSize)
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
					fmt.Println("done here")
					return DialResponse{}, err
				}
				rem -= n
			} else {
				n, err := s.Write(c.dialCharge[:rem])
				if err != nil {
					fmt.Println("done for here")
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

	if msg.GetDialResponse().GetStatus() != pb.DialResponse_ResponseStatus_OK {
		return DialResponse{Status: msg.GetDialResponse().GetStatus()}, nil
	}

	dialSuccess := false
	statuses := msg.GetDialResponse().GetDialStatuses()
	for _, st := range statuses {
		if st == pb.DialResponse_DialStaus_OK {
			dialSuccess = true
			break
		}
	}

	var attemptLocalAddr ma.Multiaddr
	if dialSuccess {
		select {
		case at := <-ch:
			fmt.Println("received nonce")
			if at.nonce == nonce {
				attemptLocalAddr = at.addr
			}
		case <-ctx.Done():
		}
	}
	fmt.Println("writing")
	results := make([]DialResult, len(addrs))
	for i := 0; i < len(addrs); i++ {
		if i >= len(statuses) {
			results[i] = DialResult{Status: -1}
			continue
		}
		results[i] = DialResult{Status: statuses[i], Addr: addrs[i]}
		if results[i].Status == pb.DialResponse_DialStaus_OK {
			if attemptLocalAddr != nil {
				results[i].LocalAddr = attemptLocalAddr
			} else {
				results[i].Status = pb.DialResponse_E_ATTEMPT_ERROR
			}
		}
	}
	return DialResponse{
		Status:  msg.GetDialResponse().GetStatus(),
		Results: results,
	}, nil
}

func (c *Client) handleDialAttempt(s network.Stream) {
	r := pbio.NewDelimitedReader(s, maxMsgSize)
	msg := &pb.DialAttempt{}
	if err := r.ReadMsg(msg); err != nil {
		return
	}

	nonce := msg.GetNonce()
	c.mu.Lock()
	ch := c.attemptQueues[nonce]
	c.mu.Unlock()
	select {
	case ch <- attempt{addr: s.Conn().LocalMultiaddr(), nonce: nonce}:
	default:
	}
}

type DialResponse struct {
	Status  pb.DialResponse_ResponseStatus
	Results []DialResult
}

type DialResult struct {
	Addr      ma.Multiaddr
	LocalAddr ma.Multiaddr
	Status    pb.DialResponse_DialStatus
}

type attempt struct {
	nonce uint64
	addr  ma.Multiaddr
}
