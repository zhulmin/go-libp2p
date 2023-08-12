package autonatv2

import (
	"context"
	"errors"
	"fmt"
	"sync"

	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/autonatv2/pbv2"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"golang.org/x/exp/rand"
)

var log = logging.Logger("autonatv2")

type AutoNAT struct {
	host          host.Host
	sub           event.Subscription
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	srv           *Server
	cli           *Client
	allowAllAddrs bool // for testing
}

func New(h host.Host, dialer host.Host, opts ...AutoNATOption) (*AutoNAT, error) {
	s := defaultSettings()
	for _, o := range opts {
		if err := o(s); err != nil {
			return nil, err
		}
	}
	sub, err := h.EventBus().Subscribe(new(event.EvtLocalReachabilityChanged))
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
		case evt := <-an.sub.Out():
			fmt.Printf("received event %s\n", evt)
			revt, ok := evt.(event.EvtLocalReachabilityChanged)
			if !ok {
				log.Errorf("Unexpected event %s of type %T", evt, evt)
			}
			if revt.Reachability == network.ReachabilityPrivate {
				an.srv.Disable()
			} else {
				an.srv.Enable()
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
	peers := an.host.Peerstore().Peers()
	idx := 0
	for _, p := range an.host.Peerstore().Peers() {
		if proto, err := an.host.Peerstore().SupportsProtocols(p, DialProtocol); len(proto) == 0 || err != nil {
			continue
		}
		peers[idx] = p
		idx++
	}
	if idx == 0 {
		return ""
	}
	peers = peers[:idx]
	rand.Shuffle(len(peers), func(i, j int) { peers[i], peers[j] = peers[j], peers[i] })
	return peers[0]
}

type Result struct {
	Addr         ma.Multiaddr
	Reachability network.Reachability
	Status       pbv2.DialStatus
}

var (
	ErrNoValidPeers = errors.New("autonat v2: No valid peers")
)
