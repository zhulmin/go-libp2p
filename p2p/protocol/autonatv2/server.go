package autonatv2

import (
	"bufio"
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/protocol/autonatv2/pb"
	"github.com/libp2p/go-msgio/pbio"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-varint"
)

type Service struct {
	h      host.Host
	dialer host.Host
}

func NewService(h, dialer host.Host) *Service {
	s := &Service{h: h, dialer: dialer}
	s.h.SetStreamHandler(dialProtocol, s.handleDialRequest)
	return s
}

func (as *Service) handleDialRequest(s network.Stream) {
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
	statuses := make([]pb.DialResponse_DialStatus, 0, len(req.GetAddrs()))
	var dialAddr ma.Multiaddr
	for _, ab := range req.GetAddrs() {
		a, err := ma.NewMultiaddrBytes(ab)
		if err != nil {
			statuses = append(statuses, pb.DialResponse_E_ADDRESS_UNKNOWN)
			continue
		}
		if !as.dialer.Network().CanDial(a) {
			statuses = append(statuses, pb.DialResponse_E_TRANSPORT_NOT_SUPPORTED)
		}
		dialAddr = a
		break
	}
	w := pbio.NewDelimitedWriter(s)
	if dialAddr == nil {
		msg := getResponseMsg(pb.DialResponse_ResponseStatus_OK, statuses)
		w.WriteMsg(msg)
		return
	}

	if dialAddr != s.Conn().RemoteMultiaddr() {
		msg.Reset()
		msg.Msg = &pb.Message_DialDataRequest{
			DialDataRequest: &pb.DialDataRequest{
				AddrIdx:  0,
				NumBytes: 20000,
			},
		}
		if err := w.WriteMsg(msg); err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("here")
		br := bufio.NewReader(s)
		ri, err := varint.ReadUvarint(br)
		if err != nil || ri != 20000 {
			fmt.Println("invalid data")
			return
		}
		remain := int(ri)
		discardBuf := make([]byte, 1024)
		for remain > 0 {
			n, err := br.Read(discardBuf)
			if err != nil {
				fmt.Println(err)
				return
			}
			remain -= n
		}
	}

	p := s.Conn().RemotePeer()
	as.dialer.Peerstore().ClearAddrs(p)
	as.dialer.Peerstore().AddAddr(p, dialAddr, peerstore.TempAddrTTL)
	err := as.dialer.Connect(context.Background(), peer.AddrInfo{ID: p})
	if err != nil {
		statuses = append(statuses, pb.DialResponse_E_DIAL_ERROR)
		msg := getResponseMsg(pb.DialResponse_ResponseStatus_OK, statuses)
		w.WriteMsg(msg)
		return
	}
	sat, err := as.dialer.NewStream(context.Background(), p, attemptProtocol)
	if err != nil {
		statuses = append(statuses, pb.DialResponse_E_ATTEMPT_ERROR)
		msg := getResponseMsg(pb.DialResponse_ResponseStatus_OK, statuses)
		w.WriteMsg(msg)
		return
	}
	wat := pbio.NewDelimitedWriter(sat)
	if err := wat.WriteMsg(&pb.DialAttempt{Nonce: req.GetNonce()}); err != nil {
		sat.Reset()
		statuses = append(statuses, pb.DialResponse_E_ATTEMPT_ERROR)
		msg := getResponseMsg(pb.DialResponse_ResponseStatus_OK, statuses)
		w.WriteMsg(msg)
		return
	}
	statuses = append(statuses, pb.DialResponse_DialStaus_OK)
	msg = getResponseMsg(pb.DialResponse_ResponseStatus_OK, statuses)
	w.WriteMsg(msg)
	return
}

func getResponseMsg(respStatus pb.DialResponse_ResponseStatus, statuses []pb.DialResponse_DialStatus) *pb.Message {
	return &pb.Message{
		Msg: &pb.Message_DialResponse{
			DialResponse: &pb.DialResponse{
				Status:       pb.DialResponse_ResponseStatus_OK,
				DialStatuses: statuses,
			},
		},
	}
}
