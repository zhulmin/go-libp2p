package swarm_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/transport"

	"github.com/libp2p/go-libp2p/p2p/transport/tcp"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/p2p/host/eventbus"
	. "github.com/libp2p/go-libp2p/p2p/net/swarm"
	swarmt "github.com/libp2p/go-libp2p/p2p/net/swarm/testing"

	"github.com/multiformats/go-multiaddr"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func newSwarmWithSubscription(t *testing.T) (*Swarm, event.Subscription) {
	t.Helper()
	bus := eventbus.NewBus()
	sw := swarmt.GenSwarm(t, swarmt.EventBus(bus))
	t.Cleanup(func() { sw.Close() })
	sub, err := bus.Subscribe(new(event.EvtPeerConnectednessChanged))
	require.NoError(t, err)
	t.Cleanup(func() { sub.Close() })
	return sw, sub
}

func checkEvent(t *testing.T, sub event.Subscription, expected event.EvtPeerConnectednessChanged) {
	t.Helper()
	select {
	case ev, ok := <-sub.Out():
		require.True(t, ok)
		evt := ev.(event.EvtPeerConnectednessChanged)
		require.Equal(t, expected.Connectedness, evt.Connectedness, "wrong connectedness state")
		require.Equal(t, expected.Peer, evt.Peer)
	case <-time.After(time.Second):
		t.Fatal("didn't get PeerConnectedness event")
	}

	// check that there are no more events
	select {
	case <-sub.Out():
		t.Fatal("didn't expect any more events")
	case <-time.After(100 * time.Millisecond):
		return
	}
}

func TestConnectednessEventsSingleConn(t *testing.T) {
	s1, sub1 := newSwarmWithSubscription(t)
	s2, sub2 := newSwarmWithSubscription(t)

	s1.Peerstore().AddAddrs(s2.LocalPeer(), []ma.Multiaddr{s2.ListenAddresses()[0]}, time.Hour)
	_, err := s1.DialPeer(context.Background(), s2.LocalPeer())
	require.NoError(t, err)

	checkEvent(t, sub1, event.EvtPeerConnectednessChanged{Peer: s2.LocalPeer(), Connectedness: network.Connected})
	checkEvent(t, sub2, event.EvtPeerConnectednessChanged{Peer: s1.LocalPeer(), Connectedness: network.Connected})

	for _, c := range s2.ConnsToPeer(s1.LocalPeer()) {
		require.NoError(t, c.Close())
	}
	checkEvent(t, sub1, event.EvtPeerConnectednessChanged{Peer: s2.LocalPeer(), Connectedness: network.NotConnected})
	checkEvent(t, sub2, event.EvtPeerConnectednessChanged{Peer: s1.LocalPeer(), Connectedness: network.NotConnected})
}

type wrappedTransport struct {
	transport.Transport
	count     atomic.Int32
	threshold int
	wait      chan struct{}
}

func newWrappedTransport(tr transport.Transport, threshold int) transport.Transport {
	return &wrappedTransport{
		Transport: tr,
		wait:      make(chan struct{}),
		threshold: threshold,
	}
}

func (t *wrappedTransport) Dial(ctx context.Context, raddr multiaddr.Multiaddr, p peer.ID) (transport.CapableConn, error) {
	fmt.Println("dial", raddr)
	conn, err := t.Transport.Dial(ctx, raddr, p)
	if int(t.count.Add(1)) == t.threshold {
		close(t.wait)
	} else {
		<-t.wait
	}
	return conn, err
}

func TestConnectednessDepup(t *testing.T) {
	addr1 := ma.StringCast("/ip4/127.0.0.1/tcp/1234")
	addr2 := ma.StringCast("/ip4/127.0.0.1/tcp/1235")
	s2 := swarmt.GenSwarm(t, swarmt.OptDisableQUIC)
	s2.Listen(addr1, addr2)

	bus := eventbus.NewBus()
	s := swarmt.GenSwarm(t, swarmt.OptDisableQUIC, swarmt.OptDisableTCP)

	sub, err := bus.Subscribe(new(event.EvtPeerConnectednessChanged))
	require.NoError(t, err)

	go func() {
		for {
			fmt.Println(<-sub.Out())
		}
	}()

	tr, err := tcp.NewTCPTransport(swarmt.GenUpgrader(t, s, nil), nil)
	require.NoError(t, err)
	require.NoError(t, s.AddTransport(newWrappedTransport(tr, 2)))

	s.Peerstore().AddAddr(s2.LocalPeer(), addr1, time.Hour)
	s.Peerstore().AddAddr(s2.LocalPeer(), addr2, time.Hour)
	_, err = s.DialPeer(context.Background(), s2.LocalPeer())
	require.NoError(t, err)
	time.Sleep(time.Second)
	require.Len(t, s.ConnsToPeer(s2.LocalPeer()), 2)
}
