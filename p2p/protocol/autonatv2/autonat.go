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
	ErrNoValidPeers = errors.New("autonat v2: No valid peers")
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
	peers         map[peer.ID]struct{}
	allowAllAddrs bool // for testing
}

func New(h host.Host, dialer host.Host, opts ...AutoNATOption) (*AutoNAT, error) {
	s := defaultSettings()
	for _, o := range opts {
		if err := o(s); err != nil {
			return nil, err
		}
	}
	sub, err := h.EventBus().Subscribe([]interface{}{
		new(event.EvtLocalReachabilityChanged),
		new(event.EvtPeerProtocolsUpdated),
		new(event.EvtPeerConnectednessChanged),
		new(event.EvtPeerIdentificationCompleted),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to event.EvtLocalReachabilityChanged: %w", err)
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
		peers:         make(map[peer.ID]struct{}),
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
			an.wg.Done()
			return
		case e := <-an.sub.Out():
			switch evt := e.(type) {
			case event.EvtLocalReachabilityChanged:
				// Enable the server only if we're publicly reachable.
				//
				// Currently this event is sent by the AutoNAT v1 module. During the
				// transition period from AutoNAT v1 to v2, there won't be enough v2 servers
				// on the network and most clients will be unable to discover a peer which
				// supports AutoNAT v2. So, we use v1 to determine reachability for the
				// transition period.
				//
				// Once there are enough v2 servers on the network for nodes to determine
				// their reachability using AutoNAT v2, we'll use Address Pipeline
				// (https://github.com/libp2p/go-libp2p/issues/2229)(to be implemented in a
				// future release) to determine reachability using v2 client and send this
				// event from Address Pipeline, if we are publicly reachable.
				if evt.Reachability == network.ReachabilityPrivate {
					an.srv.Disable()
				} else {
					an.srv.Enable()
				}
			case event.EvtPeerProtocolsUpdated:
				an.updatePeer(evt.Peer)
			case event.EvtPeerConnectednessChanged:
				an.updatePeer(evt.Peer)
			}
		}
	}
}

func (an *AutoNAT) Close() {
	an.cancel()
	an.wg.Wait()
}

// CheckReachability makes a single dial request for checking reachability. For highPriorityAddrs dial charge is paid
// if the server asks for it. For lowPriorityAddrs dial charge is rejected.
func (an *AutoNAT) CheckReachability(ctx context.Context, highPriorityAddrs []ma.Multiaddr, lowPriorityAddrs []ma.Multiaddr) ([]Result, error) {
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
	p := an.validPeer()
	if p == "" {
		return nil, ErrNoValidPeers
	}

	return an.cli.CheckReachability(ctx, p, highPriorityAddrs, lowPriorityAddrs)
}

func (an *AutoNAT) validPeer() peer.ID {
	an.mx.Lock()
	defer an.mx.Unlock()
	for p := range an.peers {
		return p
	}
	return ""
}

func (an *AutoNAT) updatePeer(p peer.ID) {
	an.mx.Lock()
	defer an.mx.Unlock()

	protos, err := an.host.Peerstore().SupportsProtocols(p, DialProtocol)
	connectedness := an.host.Network().Connectedness(p)
	if err == nil && slices.Contains(protos, DialProtocol) && connectedness == network.Connected {
		an.peers[p] = struct{}{}
	} else {
		delete(an.peers, p)
	}
}

type Result struct {
	Addr         ma.Multiaddr
	Reachability network.Reachability
	Status       pbv2.DialStatus
}
