package autonatv2

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/protocol/autonatv2/pb"
	"github.com/libp2p/go-msgio/pbio"

	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"golang.org/x/exp/rand"
)

type dataRequestPolicyFunc = func(s network.Stream, dialAddr ma.Multiaddr) bool

type Server struct {
	dialer                host.Host
	host                  host.Host
	dialDataRequestPolicy dataRequestPolicyFunc
	allowAllAddrs         bool
	limiter               *rateLimiter
	now                   func() time.Time // for tests
}

func NewServer(host, dialer host.Host, s *autoNATSettings) *Server {
	return &Server{
		dialer:                dialer,
		host:                  host,
		dialDataRequestPolicy: s.dataRequestPolicy,
		allowAllAddrs:         s.allowAllAddrs,
		limiter: &rateLimiter{
			RPM:         s.serverRPM,
			PerPeerRPM:  s.serverPerPeerRPM,
			DialDataRPM: s.serverDialDataRPM,
			now:         s.now,
		},
		now: s.now,
	}
}

func (as *Server) Enable() {
	as.host.SetStreamHandler(DialProtocol, as.handleDialRequest)
}

func (as *Server) Disable() {
	as.host.RemoveStreamHandler(DialProtocol)
}

func (as *Server) handleDialRequest(s network.Stream) {
	if err := s.Scope().SetService(ServiceName); err != nil {
		s.Reset()
		log.Debugf("failed to attach stream to service %s: %w", ServiceName, err)
		return
	}

	if err := s.Scope().ReserveMemory(maxMsgSize, network.ReservationPriorityAlways); err != nil {
		s.Reset()
		log.Debugf("failed to reserve memory for stream %s: %w", DialProtocol, err)
		return
	}
	defer s.Scope().ReleaseMemory(maxMsgSize)

	s.SetDeadline(as.now().Add(streamTimeout))
	defer s.Close()

	p := s.Conn().RemotePeer()
	r := pbio.NewDelimitedReader(s, maxMsgSize)
	var msg pb.Message
	if err := r.ReadMsg(&msg); err != nil {
		s.Reset()
		log.Debugf("failed to read request from %s: %s", p, err)
		return
	}
	if msg.GetDialRequest() == nil {
		s.Reset()
		log.Debugf("invalid message type from %s: %T", p, msg.Msg)
		return
	}

	nonce := msg.GetDialRequest().Nonce
	var dialAddr ma.Multiaddr
	var addrIdx int
	for i, ab := range msg.GetDialRequest().GetAddrs() {
		if i >= maxPeerAddresses {
			break
		}
		a, err := ma.NewMultiaddrBytes(ab)
		if err != nil {
			continue
		}
		if (!as.allowAllAddrs && !manet.IsPublicAddr(a)) ||
			(!as.dialer.Network().CanDial(a)) {
			continue
		}
		dialAddr = a
		addrIdx = i
		break
	}

	w := pbio.NewDelimitedWriter(s)
	if dialAddr == nil {
		msg = pb.Message{
			Msg: &pb.Message_DialResponse{
				DialResponse: &pb.DialResponse{
					Status: pb.DialResponse_E_DIAL_REFUSED,
				},
			},
		}
		if err := w.WriteMsg(&msg); err != nil {
			s.Reset()
			log.Debugf("failed to write response to %s: %s", p, err)
			return
		}
		return
	}

	isDialDataRequired := as.dialDataRequestPolicy(s, dialAddr)
	if !as.limiter.Accept(p, isDialDataRequired) {
		msg = pb.Message{
			Msg: &pb.Message_DialResponse{
				DialResponse: &pb.DialResponse{
					Status: pb.DialResponse_E_REQUEST_REJECTED,
				},
			},
		}
		if err := w.WriteMsg(&msg); err != nil {
			s.Reset()
			log.Debugf("failed to write response to %s: %s", p, err)
			return
		}
		log.Debugf("rejecting request from %s: rate limit exceeded", p)
		return
	}

	if isDialDataRequired {
		err := getDialData(w, r, &msg, addrIdx)
		if err != nil {
			s.Reset()
			log.Debugf("%s refused dial data request: %s", p, err)
			return
		}
	}
	status := as.dialBack(s.Conn().RemotePeer(), dialAddr, nonce)
	msg = pb.Message{
		Msg: &pb.Message_DialResponse{
			DialResponse: &pb.DialResponse{
				Status:     pb.DialResponse_OK,
				DialStatus: status,
				AddrIdx:    uint32(addrIdx),
			},
		},
	}
	if err := w.WriteMsg(&msg); err != nil {
		s.Reset()
		log.Debugf("failed to write response to %s: %s", p, err)
		return
	}
}

// amplificationAttackPrevention requests data when the peer's observed IP address is different
// from the dial back IP address
func amplificationAttackPrevention(s network.Stream, dialAddr ma.Multiaddr) bool {
	connIP, err := manet.ToIP(s.Conn().RemoteMultiaddr())
	if err != nil {
		return true
	}
	dialIP, _ := manet.ToIP(s.Conn().LocalMultiaddr()) // must be an IP multiaddr
	return !connIP.Equal(dialIP)
}

func getDialData(w pbio.Writer, r pbio.Reader, msg *pb.Message, addrIdx int) error {
	numBytes := minHandshakeSizeBytes + rand.Intn(maxHandshakeSizeBytes-minHandshakeSizeBytes)
	*msg = pb.Message{
		Msg: &pb.Message_DialDataRequest{
			DialDataRequest: &pb.DialDataRequest{
				AddrIdx:  uint32(addrIdx),
				NumBytes: uint64(numBytes),
			},
		},
	}
	if err := w.WriteMsg(msg); err != nil {
		return fmt.Errorf("dial data write: %w", err)
	}
	for remain := numBytes; remain > 0; {
		if err := r.ReadMsg(msg); err != nil {
			return fmt.Errorf("dial data read: %w", err)
		}
		if msg.GetDialDataResponse() == nil {
			return fmt.Errorf("invalid msg type %T", msg.Msg)
		}
		remain -= len(msg.GetDialDataResponse().Data)
	}
	return nil
}

func (as *Server) dialBack(p peer.ID, addr ma.Multiaddr, nonce uint64) pb.DialStatus {
	ctx, cancel := context.WithTimeout(context.Background(), dialBackDialTimeout)
	as.dialer.Peerstore().AddAddr(p, addr, peerstore.TempAddrTTL)
	defer func() {
		cancel()
		as.dialer.Network().ClosePeer(p)
		as.dialer.Peerstore().ClearAddrs(p)
		as.dialer.Peerstore().RemovePeer(p)
	}()
	s, err := as.dialer.NewStream(ctx, p, DialBackProtocol)
	if err != nil {
		return pb.DialStatus_E_DIAL_ERROR
	}
	defer s.Close()
	s.SetDeadline(as.now().Add(dialBackStreamTimeout))

	w := pbio.NewDelimitedWriter(s)
	if err := w.WriteMsg(&pb.DialBack{Nonce: nonce}); err != nil {
		s.Reset()
		return pb.DialStatus_E_DIAL_BACK_ERROR
	}

	// Since the underlying connection is on a separate dialer, it'll be closed after this function returns.
	// Connection close will drop all the queued writes. To ensure message delivery, do a close write and
	// wait a second for the peer to Close its end of the stream.
	s.CloseWrite()
	s.SetDeadline(as.now().Add(1 * time.Second))
	b := make([]byte, 1) // Read 1 byte here because 0 len reads are free to return (0, nil) immediately
	s.Read(b)

	return pb.DialStatus_OK
}

// rateLimiter implements a sliding window rate limit of requests per minute.
type rateLimiter struct {
	PerPeerRPM  int
	RPM         int
	DialDataRPM int

	mu           sync.Mutex
	reqs         []time.Time
	peerReqs     map[peer.ID][]time.Time
	dialDataReqs []time.Time
	now          func() time.Time // for tests
}

func (r *rateLimiter) Accept(p peer.ID, requiresData bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.peerReqs == nil {
		r.peerReqs = make(map[peer.ID][]time.Time)
	}

	nw := r.now()
	r.cleanup(p, nw)

	if len(r.reqs) >= r.RPM || len(r.peerReqs[p]) >= r.PerPeerRPM {
		return false
	}
	if requiresData && len(r.dialDataReqs) >= r.DialDataRPM {
		return false
	}
	r.reqs = append(r.reqs, nw)
	r.peerReqs[p] = append(r.peerReqs[p], nw)
	if requiresData {
		r.dialDataReqs = append(r.dialDataReqs, nw)
	}
	return true
}

// cleanup removes stale requests.
//
// This is fast enough in rate limited cases and the state is small enough to
// clean up quickly when blocking requests.
func (r *rateLimiter) cleanup(p peer.ID, now time.Time) {
	idx := len(r.reqs)
	for i, t := range r.reqs {
		if now.Sub(t) < time.Minute {
			idx = i
			break
		}
	}
	r.reqs = r.reqs[idx:]

	idx = len(r.dialDataReqs)
	for i, t := range r.dialDataReqs {
		if now.Sub(t) < time.Minute {
			idx = i
			break
		}
	}
	r.dialDataReqs = r.dialDataReqs[idx:]

	idx = len(r.peerReqs[p])
	for i, t := range r.peerReqs[p] {
		if now.Sub(t) < time.Minute {
			idx = i
			break
		}
	}
	r.peerReqs[p] = r.peerReqs[p][idx:]
}
