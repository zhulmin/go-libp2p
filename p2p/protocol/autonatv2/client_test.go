package autonatv2

import (
	"context"
	"fmt"
	"testing"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peerstore"
	blankhost "github.com/libp2p/go-libp2p/p2p/host/blank"
	swarmt "github.com/libp2p/go-libp2p/p2p/net/swarm/testing"
	"github.com/libp2p/go-libp2p/p2p/protocol/autonatv2/pb"
	"github.com/libp2p/go-msgio/pbio"
	ma "github.com/multiformats/go-multiaddr"
)

func TestClient(t *testing.T) {
	h := blankhost.NewBlankHost(swarmt.GenSwarm(t))
	cli := &Client{
		h:             h,
		attemptQueues: make(map[uint64]chan attempt),
	}

	p := blankhost.NewBlankHost(swarmt.GenSwarm(t))
	pp := blankhost.NewBlankHost(swarmt.GenSwarm(t))
	p.SetStreamHandler(dialProtocol, func(s network.Stream) {
		defer s.Close()
		r := pbio.NewDelimitedReader(s, maxMsgSize)
		msg := new(pb.Message)
		if err := r.ReadMsg(msg); err != nil {
			t.Fatal(err)
		}
		var addrs []ma.Multiaddr
		for _, a := range msg.GetDialRequest().GetAddrs() {
			addr, err := ma.NewMultiaddrBytes(a)
			if err != nil {
				s.Close()
				t.Fatal(err, addr)
				break
			}
			addrs = append(addrs, addr)
		}
		peer := s.Conn().RemotePeer()
		for _, a := range addrs {
			pp.Peerstore().ClearAddrs(s.Conn().RemotePeer())
			pp.Peerstore().AddAddr(peer, a, peerstore.PermanentAddrTTL)
			s, err := pp.NewStream(context.Background(), peer, attemptProtocol)
			if err != nil {
				continue
			}
			amsg := &pb.DialAttempt{Nonce: msg.GetDialRequest().Nonce}
			w := pbio.NewDelimitedWriter(s)
			err = w.WriteMsg(amsg)
			if err != nil {
				t.Fatal(err)
				break
			}
			break
		}
		msg.Reset()
		msg.Msg = &pb.Message_DialResponse{
			DialResponse: &pb.DialResponse{
				Status:       pb.DialResponse_ResponseStatus_OK,
				DialStatuses: []pb.DialResponse_DialStatus{pb.DialResponse_E_ADDRESS_UNKNOWN, pb.DialResponse_DialStaus_OK},
			},
		}
		w := pbio.NewDelimitedWriter(s)
		if err := w.WriteMsg(msg); err != nil {
			t.Fatal(err)
		}
	})

	h.Peerstore().AddAddrs(p.ID(), p.Addrs(), peerstore.PermanentAddrTTL)
	h.Peerstore().AddProtocols(p.ID(), dialProtocol)

	resp, err := cli.DialRequest(h.Addrs())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(resp)
}

func TestClientWithServer(t *testing.T) {
	h := blankhost.NewBlankHost(swarmt.GenSwarm(t))
	cli := &Client{
		h:             h,
		attemptQueues: make(map[uint64]chan attempt),
	}

	p := blankhost.NewBlankHost(swarmt.GenSwarm(t))
	pp := blankhost.NewBlankHost(swarmt.GenSwarm(t))
	NewService(p, pp)

	h.Peerstore().AddAddrs(p.ID(), p.Addrs(), peerstore.PermanentAddrTTL)
	h.Peerstore().AddProtocols(p.ID(), dialProtocol)

	resp, err := cli.DialRequest(h.Addrs())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(resp)
}

func TestClientWithServerWithCharge(t *testing.T) {
	h := blankhost.NewBlankHost(swarmt.GenSwarm(t))
	cli := &Client{
		h:             h,
		attemptQueues: make(map[uint64]chan attempt),
		dialCharge:    make([]byte, 1024),
	}

	p := blankhost.NewBlankHost(swarmt.GenSwarm(t))
	pp := blankhost.NewBlankHost(swarmt.GenSwarm(t))
	NewService(p, pp)

	h.Peerstore().AddAddrs(p.ID(), p.Addrs(), peerstore.PermanentAddrTTL)
	h.Peerstore().AddProtocols(p.ID(), dialProtocol)

	addr := ma.StringCast("/ip4/127.0.0.1/tcp/12345")
	resp, err := cli.DialRequest([]ma.Multiaddr{addr})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(resp)
}
