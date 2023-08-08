package autonatv2

import (
	"context"
	"fmt"
	"sync"

	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
)

var log = logging.Logger("autonatv2")

type AutoNAT struct {
	host   host.Host
	sub    event.Subscription
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	srv    *Server
}

func New(h host.Host, dialer network.Network) (*AutoNAT, error) {
	sub, err := h.EventBus().Subscribe(new(event.EvtLocalReachabilityChanged))
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to event.EvtLocalReachabilityChanged: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	an := &AutoNAT{
		host:   h,
		ctx:    ctx,
		cancel: cancel,
		sub:    sub,
		srv:    &Server{dialer: dialer, host: h},
	}
	an.wg.Add(1)
	go an.background()
	return an, nil
}

func (an *AutoNAT) background() {
	for {
		select {
		case <-an.ctx.Done():
			an.srv.Stop()
			return
		case evt := <-an.sub.Out():
			revt, ok := evt.(event.EvtLocalReachabilityChanged)
			if !ok {
				log.Errorf("Unexpected event %s of type %T", evt, evt)
			}
			if revt.Reachability == network.ReachabilityPrivate {
				an.srv.Stop()
			} else {
				an.srv.Start()
			}
		}
	}
}

func (an *AutoNAT) Close() {
	an.cancel()
	an.wg.Wait()
}
