package autonatv2

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/autonatv2/pbv2"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"golang.org/x/exp/rand"
	"golang.org/x/exp/slices"
)

const (
	ServiceName     = "libp2p.autonatv2"
	AttemptProtocol = "/libp2p/autonat/2/attempt"
	DialProtocol    = "/libp2p/autonat/2"

	maxMsgSize           = 8192
	streamTimeout        = time.Minute
	attemptStreamTimeout = 5 * time.Second
	attemptDialTimeout   = 30 * time.Second
)

var (
	ErrNoValidPeers = errors.New("no valid peers for autonat v2")
)

var (
	log = logging.Logger("autonatv2")
)

type AutoNAT struct {
	host          host.Host
	sub           event.Subscription
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	srv           *Server
	cli           *Client
	mx            sync.Mutex
	peers         *peersMap
	allowAllAddrs bool // for testing
}

func New(h host.Host, dialer host.Host, opts ...AutoNATOption) (*AutoNAT, error) {
	s := defaultSettings()
	for _, o := range opts {
		if err := o(s); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}
	// We are listening on event.EvtPeerProtocolsUpdated, event.EvtPeerConnectednessChanged
	// event.EvtPeerIdentificationCompleted to maintain our set of autonat supporting peers.
	//
	// We listen on event.EvtLocalReachabilityChanged to Disable the server if we are not
	// publicly reachable. Currently this event is sent by the AutoNAT v1 module. During the
	// transition period from AutoNAT v1 to v2, there won't be enough v2 servers on the network
	// and most clients will be unable to discover a peer which supports AutoNAT v2. So, we use
	// v1 to determine reachability for the transition period.
	//
	// Once there are enough v2 servers on the network for nodes to determine their reachability
	// using AutoNAT v2, we'll use Address Pipeline
	// (https://github.com/libp2p/go-libp2p/issues/2229)(to be implemented in a future release)
	// to determine reachability using v2 client and send this event from Address Pipeline, if
	// we are publicly reachable.
	sub, err := h.EventBus().Subscribe([]interface{}{
		new(event.EvtLocalReachabilityChanged),
		new(event.EvtPeerProtocolsUpdated),
		new(event.EvtPeerConnectednessChanged),
		new(event.EvtPeerIdentificationCompleted),
	})
	if err != nil {
		return nil, fmt.Errorf("event subscription failed: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())

	an := &AutoNAT{
		host:          h,
		ctx:           ctx,
		cancel:        cancel,
		sub:           sub,
		srv:           NewServer(h, dialer, s),
		cli:           NewClient(h),
		allowAllAddrs: s.allowAllAddrs,
		peers:         newPeersMap(),
	}
	an.cli.Register()

	an.wg.Add(1)
	go an.background()
	return an, nil
}

func (an *AutoNAT) background() {
	for {
		select {
		case <-an.ctx.Done():
			an.srv.Disable()
			an.peers = nil
			an.wg.Done()
			return
		case e := <-an.sub.Out():
			switch evt := e.(type) {
			case event.EvtLocalReachabilityChanged:
				if evt.Reachability == network.ReachabilityPrivate {
					an.srv.Disable()
				} else {
					an.srv.Enable()
				}
			case event.EvtPeerProtocolsUpdated:
				an.updatePeer(evt.Peer)
			case event.EvtPeerConnectednessChanged:
				an.updatePeer(evt.Peer)
			case event.EvtPeerIdentificationCompleted:
				an.updatePeer(evt.Peer)
			}
		}
	}
}

func (an *AutoNAT) Close() {
	an.cancel()
	an.wg.Wait()
}

// Result is the result of the CheckReachability call
type Result struct {
	// Idx is the index of the dialed address
	Idx int
	// Addr is the dialed address
	Addr ma.Multiaddr
	// Reachability of the dialed address
	Reachability network.Reachability
	// Status is the outcome of the dialback
	Status pbv2.DialStatus
}

// CheckReachability makes a single dial request for checking reachability. For highPriorityAddrs dial charge is paid
// if the server asks for it. For lowPriorityAddrs dial charge is rejected.
func (an *AutoNAT) CheckReachability(ctx context.Context, highPriorityAddrs []ma.Multiaddr, lowPriorityAddrs []ma.Multiaddr) (*Result, error) {
	if !an.allowAllAddrs {
		for _, a := range highPriorityAddrs {
			if !manet.IsPublicAddr(a) {
				return nil, fmt.Errorf("private address cannot be verified by autonatv2: %s", a)
			}
		}
		for _, a := range lowPriorityAddrs {
			if !manet.IsPublicAddr(a) {
				return nil, fmt.Errorf("private address cannot be verified by autonatv2: %s", a)
			}
		}
	}
	p := an.peers.GetRand()
	if p == "" {
		return nil, ErrNoValidPeers
	}

	res, err := an.cli.CheckReachability(ctx, p, highPriorityAddrs, lowPriorityAddrs)
	if err != nil {
		log.Debugf("reachability check with %s failed, err: %s", p, err)
		return nil, fmt.Errorf("reachability check with %s failed: %w", p, err)
	}
	log.Debugf("reachability check with %s successful", p)
	return res, nil
}

func (an *AutoNAT) updatePeer(p peer.ID) {
	an.mx.Lock()
	defer an.mx.Unlock()

	protos, err := an.host.Peerstore().SupportsProtocols(p, DialProtocol)
	connectedness := an.host.Network().Connectedness(p)
	if err == nil && slices.Contains(protos, DialProtocol) && connectedness == network.Connected {
		an.peers.Put(p)
	} else {
		an.peers.Remove(p)
	}
}

// peersMap provides random access to a set of peers. This is useful when the map iteration order is
// not sufficiently random.
type peersMap struct {
	peerIdx map[peer.ID]int
	peers   []peer.ID
}

func newPeersMap() *peersMap {
	return &peersMap{
		peerIdx: make(map[peer.ID]int),
		peers:   make([]peer.ID, 0),
	}
}

func (p *peersMap) GetRand() peer.ID {
	if len(p.peers) == 0 {
		return ""
	}
	return p.peers[rand.Intn(len(p.peers))]
}

func (p *peersMap) Put(pid peer.ID) {
	if _, ok := p.peerIdx[pid]; ok {
		return
	}
	p.peers = append(p.peers, pid)
	p.peerIdx[pid] = len(p.peers) - 1
}

func (p *peersMap) Remove(pid peer.ID) {
	idx, ok := p.peerIdx[pid]
	if !ok {
		return
	}
	delete(p.peerIdx, pid)
	p.peers[idx] = p.peers[len(p.peers)-1]
	p.peerIdx[p.peers[idx]] = idx
	p.peers = p.peers[:len(p.peers)-1]
}
