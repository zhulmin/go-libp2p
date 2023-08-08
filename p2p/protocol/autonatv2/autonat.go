package autonatv2

import (
	"context"
	"fmt"
	"sync"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/host"
)

type AutoNAT struct {
	host   host.Host
	sub    event.Subscription
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func New(h host.Host) (*AutoNAT, error) {
	sub, err := h.EventBus().Subscribe(new(event.EvtLocalReachabilityChanged))
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to event.EvtLocalReachabilityChanged: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	an := &AutoNAT{host: h, ctx: ctx, cancel: cancel, sub: sub}

	an.wg.Add(1)
	go an.background()
	return an, nil
}

func (an *AutoNAT) background() {
	for {
		select {
		case <-an.ctx.Done():
			return
		case <-an.sub.Out():
			//fix reachability
		}
	}
}

func (an *AutoNAT) Close() {
	an.cancel()
	an.wg.Wait()
}
