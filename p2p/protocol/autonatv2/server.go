package autonatv2

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/protocol/autonatv2/pbv2"
	"github.com/libp2p/go-msgio/pbio"
	"github.com/multiformats/go-multiaddr"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"golang.org/x/exp/rand"
)

const DialProtocol = "/libp2p/autonat/2"

type dataRequestPolicyFunc = func(s network.Stream, dialAddr ma.Multiaddr) bool

type Server struct {
	dialer            host.Host
	host              host.Host
	dataRequestPolicy dataRequestPolicyFunc
	allowAllAddrs     bool
}

func NewServer(host, dialer host.Host, dataRequestPolicy dataRequestPolicyFunc, allowAllAddrs bool) *Server {
	drp := defaultDataRequestPolicy
	if dataRequestPolicy != nil {
		drp = dataRequestPolicy
	}
	return &Server{dialer: dialer, host: host, dataRequestPolicy: drp, allowAllAddrs: allowAllAddrs}
}

func (as *Server) Start() {
	as.host.SetStreamHandler(DialProtocol, as.handleDialRequest)
}

func (as *Server) Stop() {
	as.host.RemoveStreamHandler(DialProtocol)
}

func (as *Server) handleDialRequest(s network.Stream) {
	if err := s.Scope().SetService(ServiceName); err != nil {
		log.Debugf("error attaching stream to service %s: %s", ServiceName, err)
		s.Reset()
		return
	}

	if err := s.Scope().ReserveMemory(maxMsgSize, network.ReservationPriorityAlways); err != nil {
		log.Debugf("error reserving memory for autonatv2 stream: %s", err)
		s.Reset()
		return
	}
	defer s.Scope().ReleaseMemory(maxMsgSize)

	s.SetDeadline(time.Now().Add(time.Minute))
	defer s.Close()

	r := pbio.NewDelimitedReader(s, maxMsgSize)
	msg := &pbv2.Message{}
	if err := r.ReadMsg(msg); err != nil {
		s.Reset()
		log.Debugf("error reading %s request: %s", DialProtocol, err)
		return
	}
	if msg.GetDialRequest() == nil {
		s.Reset()
		log.Debugf("invalid message type: %T", msg.Msg)
		return
	}
	nonce := msg.GetDialRequest().Nonce
	statuses := make([]pbv2.DialStatus, 0, len(msg.GetDialRequest().GetAddrs()))
	var dialAddr ma.Multiaddr
	for _, ab := range msg.GetDialRequest().GetAddrs() {
		a, err := multiaddr.NewMultiaddrBytes(ab)
		if err != nil {
			statuses = append(statuses, pbv2.DialStatus_E_ADDRESS_UNKNOWN)
			continue
		}
		if !as.allowAllAddrs && !manet.IsPublicAddr(a) {
			statuses = append(statuses, pbv2.DialStatus_E_DIAL_REFUSED)
			continue
		}
		if _, err := a.ValueForProtocol(ma.P_CIRCUIT); err == nil {
			statuses = append(statuses, pbv2.DialStatus_E_DIAL_REFUSED)
			continue
		}
		if !as.dialer.Network().CanDial(a) {
			statuses = append(statuses, pbv2.DialStatus_E_TRANSPORT_NOT_SUPPORTED)
			continue
		}
		dialAddr = a
		break
	}

	w := pbio.NewDelimitedWriter(s)
	if dialAddr == nil {
		msg := getResponseMsg(pbv2.DialResponse_ResponseStatus_OK, statuses)
		w.WriteMsg(msg)
		return
	}

	if as.dataRequestPolicy(s, dialAddr) {
		msg.Reset()
		err := getDialData(w, r, len(statuses))
		if err != nil {
			s.Reset()
			log.Debugf("peer refused data request: %s", err)
			return
		}
	}

	status := as.attemptDial(s.Conn().RemotePeer(), dialAddr, nonce)
	statuses = append(statuses, status)
	msg.Reset()
	msg = getResponseMsg(pbv2.DialResponse_ResponseStatus_OK, statuses)
	if err := w.WriteMsg(msg); err != nil {
		s.Reset()
		return
	}
}

// defaultDataRequestPolicy requests data when the peer's observed IP address is different
// from the dial back IP address, thus preventing amplification attacks
func defaultDataRequestPolicy(s network.Stream, dialAddr ma.Multiaddr) bool {
	connIP, err := manet.ToIP(s.Conn().RemoteMultiaddr())
	if err != nil {
		return true
	}
	dialIP, _ := manet.ToIP(s.Conn().LocalMultiaddr()) // must be an IP multiaddr
	return !connIP.Equal(dialIP)
}

func getDialData(w pbio.Writer, r pbio.Reader, addrIdx int) error {
	numBytes := rand.Intn(70000) + 30000
	msg := &pbv2.Message{Msg: &pbv2.Message_DialDataRequest{
		DialDataRequest: &pbv2.DialDataRequest{
			AddrIdx:  uint32(addrIdx),
			NumBytes: uint64(numBytes),
		},
	}}
	if err := w.WriteMsg(msg); err != nil {
		return fmt.Errorf("error requesting dial data: %w", err)
	}
	remain := numBytes
	for remain > 0 {
		msg.Reset()
		if err := r.ReadMsg(msg); err != nil {
			return fmt.Errorf("error reading dial data: %w", err)
		}
		if msg.GetDialDataResponse() == nil {
			return fmt.Errorf("invalid msg type. expected DialDataResponse, got %T", msg.Msg)
		}
		remain -= len(msg.GetDialDataResponse().Data)
	}
	return nil
}

func (as *Server) attemptDial(p peer.ID, addr ma.Multiaddr, nonce uint64) pbv2.DialStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	as.dialer.Peerstore().AddAddr(p, addr, peerstore.TempAddrTTL)
	defer func() {
		cancel()
		as.dialer.Network().ClosePeer(p)
		as.dialer.Peerstore().ClearAddrs(p)
	}()
	s, err := as.dialer.NewStream(ctx, p, AttemptProtocol)
	if err != nil {
		return pbv2.DialStatus_E_DIAL_ERROR
	}
	defer s.Close()
	s.SetDeadline(time.Now().Add(5 * time.Second))

	w := pbio.NewDelimitedWriter(s)
	if err := w.WriteMsg(&pbv2.DialAttempt{Nonce: nonce}); err != nil {
		s.Reset()
		return pbv2.DialStatus_E_ATTEMPT_ERROR
	}
	// s.Close() here might discard the message
	s.CloseWrite()
	s.SetDeadline(time.Now().Add(1 * time.Second))
	b := make([]byte, 1)
	s.Read(b)

	return pbv2.DialStatus_OK
}

func getResponseMsg(respStatus pbv2.DialResponse_ResponseStatus, statuses []pbv2.DialStatus) *pbv2.Message {
	return &pbv2.Message{
		Msg: &pbv2.Message_DialResponse{
			DialResponse: &pbv2.DialResponse{
				Status:       pbv2.DialResponse_ResponseStatus_OK,
				DialStatuses: statuses,
			},
		},
	}
}
