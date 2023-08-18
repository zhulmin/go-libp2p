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

//go:generate protoc --go_out=. --go_opt=Mpbv2/autonat.proto=./pbv2 pbv2/autonat.proto

// Client implements the client for making dial requests for AutoNAT v2. It verifies successful
// dials and provides an option to send data for dial requests.
type Client struct {
	host     host.Host
	dialData []byte

	mu sync.Mutex
	// dialBackQueues maps nonce to the channel for providing the local multiaddr of the connection
	// the nonce was received on
	dialBackQueues map[uint64]chan ma.Multiaddr
}

func NewClient(h host.Host) *Client {
	return &Client{host: h, dialData: make([]byte, 4096), dialBackQueues: make(map[uint64]chan ma.Multiaddr)}
}

// CheckReachability verifies address reachability with a AutoNAT v2 server p. It'll provide dial data for dialing high
// priority addresses and not for low priority addresses.
func (ac *Client) CheckReachability(ctx context.Context, p peer.ID, highPriorityAddrs []ma.Multiaddr, lowPriorityAddrs []ma.Multiaddr) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, streamTimeout)
	defer cancel()

	s, err := ac.host.NewStream(ctx, p, DialProtocol)
	if err != nil {
		return nil, fmt.Errorf("open %s stream: %w", DialProtocol, err)
	}

	if err := s.Scope().SetService(ServiceName); err != nil {
		s.Reset()
		return nil, fmt.Errorf("attach stream %s to service %s: %w", DialProtocol, ServiceName, err)
	}

	if err := s.Scope().ReserveMemory(maxMsgSize, network.ReservationPriorityAlways); err != nil {
		s.Reset()
		return nil, fmt.Errorf("failed to reserve memory for stream %s: %w", DialProtocol, err)
	}
	defer s.Scope().ReleaseMemory(maxMsgSize)

	s.SetDeadline(time.Now().Add(streamTimeout))
	defer s.Close()

	nonce := rand.Uint64()
	ch := make(chan ma.Multiaddr, 1)
	ac.mu.Lock()
	ac.dialBackQueues[nonce] = ch
	ac.mu.Unlock()
	defer func() {
		ac.mu.Lock()
		delete(ac.dialBackQueues, nonce)
		ac.mu.Unlock()
	}()

	msg := newDialRequest(highPriorityAddrs, lowPriorityAddrs, nonce)
	w := pbio.NewDelimitedWriter(s)
	if err := w.WriteMsg(&msg); err != nil {
		s.Reset()
		return nil, fmt.Errorf("dial request write: %w", err)
	}

	r := pbio.NewDelimitedReader(s, maxMsgSize)
	if err := r.ReadMsg(&msg); err != nil {
		s.Reset()
		return nil, fmt.Errorf("dial msg read: %w", err)
	}

	switch {
	case msg.GetDialResponse() != nil:
		break
	case msg.GetDialDataRequest() != nil:
		if int(msg.GetDialDataRequest().AddrIdx) >= len(highPriorityAddrs) {
			s.Reset()
			return nil, fmt.Errorf("dial data requested for low priority address")
		}
		if msg.GetDialDataRequest().NumBytes > maxHandshakeSizeBytes {
			s.Reset()
			return nil, fmt.Errorf("dial data requested too high: %d", msg.GetDialDataRequest().NumBytes)
		}
		if err := ac.sendDialData(msg.GetDialDataRequest(), w, &msg); err != nil {
			s.Reset()
			return nil, fmt.Errorf("dial data send: %w", err)
		}
		if err := r.ReadMsg(&msg); err != nil {
			s.Reset()
			return nil, fmt.Errorf("dial response read: %w", err)
		}
		if msg.GetDialResponse() == nil {
			s.Reset()
			return nil, fmt.Errorf("invalid response type: %T", msg.Msg)
		}
	default:
		s.Reset()
		return nil, fmt.Errorf("invalid msg type: %T", msg.Msg)
	}

	resp := msg.GetDialResponse()
	if resp.GetStatus() != pbv2.DialResponse_ResponseStatus_OK {
		return nil, fmt.Errorf("dial request failed: status %d %s", resp.GetStatus(),
			pbv2.DialStatus_name[int32(resp.GetStatus())])
	}
	if resp.GetDialStatus() == pbv2.DialStatus_E_INTERNAL_ERROR {
		return nil, fmt.Errorf("dial request failed: received invalid dial status 0")
	}

	var dialBackAddr ma.Multiaddr
	if resp.GetDialStatus() == pbv2.DialStatus_OK && int(resp.AddrIdx) < len(highPriorityAddrs)+len(lowPriorityAddrs) {
		timer := time.NewTimer(dialBackStreamTimeout)
		select {
		case at := <-ch:
			dialBackAddr = at
		case <-ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
	}
	return ac.newResults(resp, highPriorityAddrs, lowPriorityAddrs, dialBackAddr)
}

func (ac *Client) newResults(resp *pbv2.DialResponse, highPriorityAddrs []ma.Multiaddr, lowPriorityAddrs []ma.Multiaddr, dialBackAddr ma.Multiaddr) (*Result, error) {
	if resp.DialStatus == pbv2.DialStatus_E_DIAL_REFUSED {
		return &Result{Idx: -1, Reachability: network.ReachabilityUnknown, Status: pbv2.DialStatus_E_DIAL_REFUSED}, nil
	}
	if int(resp.AddrIdx) >= len(highPriorityAddrs)+len(lowPriorityAddrs) {
		return nil, fmt.Errorf("addrIdx out of range: %d 0-%d", resp.AddrIdx, len(highPriorityAddrs)+len(lowPriorityAddrs)-1)
	}

	idx := int(resp.AddrIdx)
	var addr ma.Multiaddr
	if idx < len(highPriorityAddrs) {
		addr = highPriorityAddrs[idx]
	} else {
		addr = lowPriorityAddrs[idx-len(highPriorityAddrs)]
	}

	rch := network.ReachabilityUnknown
	status := resp.DialStatus
	switch status {
	case pbv2.DialStatus_OK:
		if areAddrsConsistent(dialBackAddr, addr) {
			rch = network.ReachabilityPublic
		} else {
			status = pbv2.DialStatus_E_DIAL_BACK_ERROR
		}
	case pbv2.DialStatus_E_DIAL_ERROR:
		rch = network.ReachabilityPrivate
	}
	return &Result{
		Idx:          idx,
		Addr:         addr,
		Reachability: rch,
		Status:       status,
	}, nil
}

func (ac *Client) sendDialData(req *pbv2.DialDataRequest, w pbio.Writer, msg *pbv2.Message) error {
	nb := req.GetNumBytes()
	ddResp := &pbv2.DialDataResponse{Data: ac.dialData}
	*msg = pbv2.Message{
		Msg: &pbv2.Message_DialDataResponse{
			DialDataResponse: ddResp,
		},
	}
	for remain := int(nb); remain > 0; {
		end := remain
		if end > len(ac.dialData) {
			end = len(ac.dialData)
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
	ac.host.SetStreamHandler(DialBackProtocol, ac.handleDialBack)
}

func (ac *Client) handleDialBack(s network.Stream) {
	if err := s.Scope().SetService(ServiceName); err != nil {
		log.Debugf("failed to attach stream to service %s: %w", ServiceName, err)
		s.Reset()
		return
	}

	if err := s.Scope().ReserveMemory(maxMsgSize, network.ReservationPriorityAlways); err != nil {
		log.Debugf("failed to reserve memory for stream %s: %w", DialBackProtocol, err)
		s.Reset()
		return
	}
	defer s.Scope().ReleaseMemory(maxMsgSize)

	s.SetDeadline(time.Now().Add(dialBackStreamTimeout))
	defer s.Close()

	r := pbio.NewDelimitedReader(s, maxMsgSize)
	var msg pbv2.DialBack
	if err := r.ReadMsg(&msg); err != nil {
		log.Debugf("failed to read dialback msg from %s: %s", s.Conn().RemotePeer(), err)
		s.Reset()
		return
	}
	nonce := msg.GetNonce()

	ac.mu.Lock()
	ch := ac.dialBackQueues[nonce]
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
