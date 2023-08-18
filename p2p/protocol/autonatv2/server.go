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
	"github.com/libp2p/go-libp2p/p2p/protocol/autonatv2/pbv2"
	"github.com/libp2p/go-msgio/pbio"

	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"golang.org/x/exp/rand"
)

type dataRequestPolicyFunc = func(s network.Stream, dialAddr ma.Multiaddr) bool

const (
	maxHandshakeSizeBytes = 100_000
	minHandshakeSizeBytes = 30_000
)

type Server struct {
	dialer            host.Host
	host              host.Host
	dataRequestPolicy dataRequestPolicyFunc
	allowAllAddrs     bool
	limiter           *rateLimiter
	now               func() time.Time // for tests
}

func NewServer(host, dialer host.Host, s *autoNATSettings) *Server {
	return &Server{
		dialer:            dialer,
		host:              host,
		dataRequestPolicy: s.dataRequestPolicy,
		allowAllAddrs:     s.allowAllAddrs,
		limiter: &rateLimiter{
			RPM:        s.serverRPM,
			RPMPerPeer: s.serverRPMPerPeer,
			now:        s.now,
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
	var msg pbv2.Message
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
	if !as.limiter.Accept(p) {
		s.Reset()
		log.Debugf("rejecting request from %s: rate limit exceeded", p)
		return
	}

	nonce := msg.GetDialRequest().Nonce
	var dialAddr ma.Multiaddr
	var addrIdx int
	for i, ab := range msg.GetDialRequest().GetAddrs() {
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
		msg = pbv2.Message{
			Msg: &pbv2.Message_DialResponse{
				DialResponse: &pbv2.DialResponse{
					Status:     pbv2.DialResponse_ResponseStatus_OK,
					DialStatus: pbv2.DialStatus_E_DIAL_REFUSED,
					// send an invalid index to prevent accidental misuse
					AddrIdx: uint32(len(msg.GetDialRequest().Addrs)),
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

	if as.dataRequestPolicy(s, dialAddr) {
		err := runAmplificationAttackPrevention(w, r, &msg, addrIdx)
		if err != nil {
			s.Reset()
			log.Debugf("%s refused dial data request: %s", p, err)
			return
		}
	}
	status := as.attemptDial(s.Conn().RemotePeer(), dialAddr, nonce)
	msg = pbv2.Message{
		Msg: &pbv2.Message_DialResponse{
			DialResponse: &pbv2.DialResponse{
				Status:     pbv2.DialResponse_ResponseStatus_OK,
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

func runAmplificationAttackPrevention(w pbio.Writer, r pbio.Reader, msg *pbv2.Message, addrIdx int) error {
	numBytes := minHandshakeSizeBytes + rand.Intn(maxHandshakeSizeBytes-minHandshakeSizeBytes)
	*msg = pbv2.Message{
		Msg: &pbv2.Message_DialDataRequest{
			DialDataRequest: &pbv2.DialDataRequest{
				AddrIdx:  uint32(addrIdx),
				NumBytes: uint64(numBytes),
			},
		},
	}
	if err := w.WriteMsg(msg); err != nil {
		return fmt.Errorf("dial data write: %w", err)
	}
	remain := numBytes
	for remain > 0 {
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

func (as *Server) attemptDial(p peer.ID, addr ma.Multiaddr, nonce uint64) pbv2.DialStatus {
	ctx, cancel := context.WithTimeout(context.Background(), attemptDialTimeout)
	as.dialer.Peerstore().AddAddr(p, addr, peerstore.TempAddrTTL)
	defer func() {
		cancel()
		as.dialer.Network().ClosePeer(p)
		as.dialer.Peerstore().ClearAddrs(p)
		as.dialer.Peerstore().RemovePeer(p)
	}()
	s, err := as.dialer.NewStream(ctx, p, AttemptProtocol)
	if err != nil {
		return pbv2.DialStatus_E_DIAL_ERROR
	}
	defer s.Close()
	s.SetDeadline(as.now().Add(attemptStreamTimeout))

	w := pbio.NewDelimitedWriter(s)
	if err := w.WriteMsg(&pbv2.DialAttempt{Nonce: nonce}); err != nil {
		s.Reset()
		return pbv2.DialStatus_E_ATTEMPT_ERROR
	}

	// Since the underlying connection is on a separate dialer, it'll be closed after this function returns.
	// Connection close will drop all the queued writes. To ensure message delivery, do a close write and
	// wait a second for the peer to Close its end of the stream.
	s.CloseWrite()
	s.SetDeadline(as.now().Add(1 * time.Second))
	b := make([]byte, 1) // Read 1 byte here because 0 len reads are free to return (0, nil) immediately
	s.Read(b)

	return pbv2.DialStatus_OK
}

// rateLimiter implements a sliding window rate limit of requests per minute.
type rateLimiter struct {
	RPMPerPeer int
	RPM        int

	mu       sync.Mutex
	reqs     []time.Time
	peerReqs map[peer.ID][]time.Time
	now      func() time.Time // for tests
}

func (r *rateLimiter) Accept(p peer.ID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.peerReqs == nil {
		r.peerReqs = make(map[peer.ID][]time.Time)
	}

	nw := r.now()
	r.cleanup(p, nw)

	if len(r.reqs) >= r.RPM || len(r.peerReqs[p]) >= r.RPMPerPeer {
		return false
	}
	r.reqs = append(r.reqs, nw)
	r.peerReqs[p] = append(r.peerReqs[p], nw)
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

	idx = len(r.peerReqs[p])
	for i, t := range r.peerReqs[p] {
		if now.Sub(t) < time.Minute {
			idx = i
			break
		}
	}
	r.peerReqs[p] = r.peerReqs[p][idx:]
}
