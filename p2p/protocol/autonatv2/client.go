package autonatv2

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/autonatv2/pbv2"
	"github.com/libp2p/go-msgio/pbio"
	ma "github.com/multiformats/go-multiaddr"
	"golang.org/x/exp/rand"
	"golang.org/x/exp/slices"
)

const (
	ServiceName     = "libp2p.autonatv2"
	AttemptProtocol = "/libp2p/autonat/2/attempt"
	maxMsgSize      = 8192
)

//go:generate protoc --go_out=. --go_opt=Mpbv2/autonat.proto=./pbv2 pbv2/autonat.proto

type Client struct {
	host       host.Host
	dialCharge []byte

	mu            sync.Mutex
	attemptQueues map[uint64]chan ma.Multiaddr
}

func NewClient(h host.Host) *Client {
	return &Client{host: h, dialCharge: make([]byte, 4096), attemptQueues: make(map[uint64]chan ma.Multiaddr)}
}

func (ac *Client) CheckReachability(ctx context.Context, p peer.ID, highPriorityAddrs []ma.Multiaddr, lowPriorityAddrs []ma.Multiaddr) ([]Result, error) {
	ctx, cancel := context.WithTimeout(ctx, streamTimeout)
	defer cancel()

	s, err := ac.host.NewStream(ctx, p, DialProtocol)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s stream with %s: %w", DialProtocol, p, err)
	}

	if err := s.Scope().SetService(ServiceName); err != nil {
		log.Debugf("error attaching stream to service: %s", err)
		s.Reset()
		return nil, fmt.Errorf("failed to attach stream to service %s: %w", ServiceName, err)
	}

	if err := s.Scope().ReserveMemory(maxMsgSize, network.ReservationPriorityAlways); err != nil {
		log.Debugf("error reserving memory for stream: %s", err)
		s.Reset()
		return nil, fmt.Errorf("failed to reserve memory for stream %s: %w", DialProtocol, err)
	}
	defer s.Scope().ReleaseMemory(maxMsgSize)

	s.SetDeadline(time.Now().Add(streamTimeout))
	defer s.Close()

	nonce := rand.Uint64()
	ch := make(chan ma.Multiaddr, 1)
	ac.mu.Lock()
	ac.attemptQueues[nonce] = ch
	ac.mu.Unlock()
	defer func() {
		ac.mu.Lock()
		delete(ac.attemptQueues, nonce)
		ac.mu.Unlock()
	}()

	msg := newDialRequest(highPriorityAddrs, lowPriorityAddrs, nonce)
	w := pbio.NewDelimitedWriter(s)
	if err := w.WriteMsg(&msg); err != nil {
		s.Reset()
		return nil, fmt.Errorf("failed to write dial request: %w", err)
	}

	r := pbio.NewDelimitedReader(s, maxMsgSize)
	if err := r.ReadMsg(&msg); err != nil {
		s.Reset()
		return nil, fmt.Errorf("failed to read dial msg on %s: %w", DialProtocol, err)
	}

	switch {
	case msg.GetDialResponse() != nil:
		break
	case msg.GetDialDataRequest() != nil:
		req := msg.GetDialDataRequest()
		if int(req.AddrIdx) >= len(highPriorityAddrs) {
			s.Reset()
			return nil, fmt.Errorf("not providing dial data for low priority address")
		}
		if err := ac.sendDialData(msg.GetDialDataRequest(), w, &msg); err != nil {
			s.Reset()
			return nil, fmt.Errorf("failed to send dial data: %w", err)
		}
		if err := r.ReadMsg(&msg); err != nil {
			s.Reset()
			return nil, fmt.Errorf("failed to read dial response: %w", err)
		}
		if msg.GetDialResponse() == nil {
			s.Reset()
			return nil, fmt.Errorf("invalid response type %T", msg.Msg)
		}
	default:
		s.Reset()
		return nil, fmt.Errorf("invalid msg type %T", msg.Msg)
	}

	resp := msg.GetDialResponse()
	if resp.GetStatus() != pbv2.DialResponse_ResponseStatus_OK {
		s.Reset()
		return nil, fmt.Errorf("dial request failed: status: %s", pbv2.DialStatus_name[int32(resp.GetStatus())])
	}

	var attempt ma.Multiaddr
	for _, st := range resp.GetDialStatuses() {
		timer := time.NewTimer(attemptStreamTimeout)
		if st == pbv2.DialStatus_OK {
			select {
			case at := <-ch:
				attempt = at
			case <-ctx.Done():
			case <-timer.C:
			}
			break
		}
		timer.Stop()
	}

	return ac.newResults(resp.GetDialStatuses(), highPriorityAddrs, lowPriorityAddrs, attempt), nil
}

func (ac *Client) newResults(ds []pbv2.DialStatus, highPriorityAddrs []ma.Multiaddr, lowPriorityAddrs []ma.Multiaddr, attempt ma.Multiaddr) []Result {
	res := make([]Result, len(highPriorityAddrs)+len(lowPriorityAddrs))
	for i := 0; i < len(res); i++ {
		var addr ma.Multiaddr
		if i < len(highPriorityAddrs) {
			addr = highPriorityAddrs[i]
		} else {
			addr = lowPriorityAddrs[i-len(highPriorityAddrs)]
		}
		rch := network.ReachabilityUnknown
		status := pbv2.DialStatus_SKIPPED
		if i < len(ds) {
			switch ds[i] {
			case pbv2.DialStatus_OK:
				if areAddrsConsistent(attempt, addr) {
					status = pbv2.DialStatus_OK
					rch = network.ReachabilityPublic
				} else {
					status = pbv2.DialStatus_E_ATTEMPT_ERROR
					rch = network.ReachabilityUnknown
				}
			case pbv2.DialStatus_E_DIAL_ERROR:
				rch = network.ReachabilityPrivate
			default:
				status = ds[i]
				rch = network.ReachabilityUnknown
			}
		}
		res[i] = Result{Addr: addr, Reachability: rch, Status: status}
	}
	return res
}

func (ac *Client) sendDialData(req *pbv2.DialDataRequest, w pbio.Writer, msg *pbv2.Message) error {
	nb := req.GetNumBytes()
	ddResp := &pbv2.DialDataResponse{Data: ac.dialCharge}
	*msg = pbv2.Message{
		Msg: &pbv2.Message_DialDataResponse{
			DialDataResponse: ddResp,
		},
	}
	for remain := int(nb); remain > 0; {
		end := remain
		if end > len(ac.dialCharge) {
			end = len(ac.dialCharge)
		}
		ddResp.Data = ddResp.Data[:end]
		if err := w.WriteMsg(msg); err != nil {
			return err
		}
		remain -= end
	}
	return nil
}

func newDialRequest(highPriorityAddrs, lowPriorityAddrs []ma.Multiaddr, nonce uint64) pbv2.Message {
	addrbs := make([][]byte, len(highPriorityAddrs)+len(lowPriorityAddrs))
	for i, a := range highPriorityAddrs {
		addrbs[i] = a.Bytes()
	}
	for i, a := range lowPriorityAddrs {
		addrbs[len(highPriorityAddrs)+i] = a.Bytes()
	}
	return pbv2.Message{
		Msg: &pbv2.Message_DialRequest{
			DialRequest: &pbv2.DialRequest{
				Addrs: addrbs,
				Nonce: nonce,
			},
		},
	}
}

func (ac *Client) Register() {
	ac.host.SetStreamHandler(AttemptProtocol, ac.handleDialAttempt)
}

func (ac *Client) handleDialAttempt(s network.Stream) {
	if err := s.Scope().SetService(ServiceName); err != nil {
		log.Errorf("error attaching stream to service %s: %s", ServiceName, err)
		s.Reset()
		return
	}

	if err := s.Scope().ReserveMemory(maxMsgSize, network.ReservationPriorityAlways); err != nil {
		log.Errorf("error reserving memory for autonatv2 attempt stream: %s", err)
		s.Reset()
		return
	}
	defer s.Scope().ReleaseMemory(maxMsgSize)

	s.SetDeadline(time.Now().Add(attemptStreamTimeout))
	defer s.Close()

	r := pbio.NewDelimitedReader(s, maxMsgSize)
	msg := &pbv2.DialAttempt{}
	if err := r.ReadMsg(msg); err != nil {
		log.Debugf("error reading dial attempt msg: %s", err)
		s.Reset()
		return
	}
	nonce := msg.GetNonce()

	ac.mu.Lock()
	ch := ac.attemptQueues[nonce]
	ac.mu.Unlock()
	select {
	case ch <- s.Conn().LocalMultiaddr():
	default:
	}
}

func areAddrsConsistent(a, b ma.Multiaddr) bool {
	if a == nil || b == nil {
		return false
	}
	// TODO: handle NAT64
	aprotos := a.Protocols()
	bprotos := b.Protocols()
	return slices.EqualFunc(aprotos, bprotos, func(p1, p2 ma.Protocol) bool { return p1.Code == p2.Code })
}
