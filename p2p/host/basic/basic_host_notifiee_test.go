package basichost

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p-core/event"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"

	swarmt "github.com/libp2p/go-libp2p-swarm/testing"
	"github.com/stretchr/testify/require"
)

func TestBasicHostNotifieeSimple(t *testing.T) {
	ctx := context.Background()
	h1 := New(swarmt.GenSwarm(t, ctx))
	defer h1.Close()
	h2 := New(swarmt.GenSwarm(t, ctx))
	defer h2.Close()

	// subscribe for notifications on h1
	s, err := h1.EventBus().Subscribe(&event.EvtPeerConnectednessChanged{})
	defer s.Close()
	require.NoError(t, err)

	// connect to h2 so we get the first notificaion
	require.NoError(t, h1.Connect(ctx, peer.AddrInfo{h2.ID(), h2.Addrs()}))
	select {
	case e := <-s.Out():
		evt, ok := e.(event.EvtPeerConnectednessChanged)
		require.True(t, ok)
		require.Equal(t, network.Connected, evt.Connectedness)
		require.Equal(t, h2.ID(), evt.Peer)
	case <-time.After(1 * time.Second):
		t.Fatal("did not get notification")
	}

	// connected -> connected does not result in a notification
	(*basicHostNotifiee)(h1).Connected(h1.network, h1.network.ConnsToPeer(h2.ID())[0])
	select {
	case <-s.Out():
		t.Fatal("should not receive any event")
	case <-time.After(1 * time.Second):
	}

	(*basicHostNotifiee)(h1).Disconnected(h1.network, h1.network.ConnsToPeer(h2.ID())[0])
	select {
	case <-s.Out():
		t.Fatal("should not receive any event")
	case <-time.After(1 * time.Second):
	}

	// disconnect so we get a notification
	require.NoError(t, h1.Network().ClosePeer(h2.ID()))
	select {
	case e := <-s.Out():
		evt, ok := e.(event.EvtPeerConnectednessChanged)
		require.True(t, ok)
		require.Equal(t, network.NotConnected, evt.Connectedness)
		require.Equal(t, h2.ID(), evt.Peer)
	case <-time.After(1 * time.Second):
		t.Fatal("did not get disconnect notification")
	}
}

func TestBasicHostNotifieeConcurrent(t *testing.T) {
	ctx := context.Background()
	h1 := New(swarmt.GenSwarm(t, ctx))
	defer h1.Close()
	h2 := New(swarmt.GenSwarm(t, ctx))
	defer h2.Close()

	// subscribe for notifications on h1
	s, err := h1.EventBus().Subscribe(&event.EvtPeerConnectednessChanged{})
	defer s.Close()
	require.NoError(t, err)

	// now make simultaneous connect<->disconnect from both sides
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(3)
		go func() {
			require.NoError(t, h1.Connect(ctx, peer.AddrInfo{h2.ID(), h2.Addrs()}))
			wg.Done()
		}()

		go func() {
			require.NoError(t, h2.network.ClosePeer(h1.ID()))
			wg.Done()
		}()

		go func() {
			require.NoError(t, h2.Connect(ctx, peer.AddrInfo{h1.ID(), h1.Addrs()}))
			wg.Done()
		}()
	}
	wg.Wait()

	var finalState event.EvtPeerConnectednessChanged
LOOP:
	for {
		select {
		case e := <-s.Out():
			evt, ok := e.(event.EvtPeerConnectednessChanged)
			require.True(t, ok)
			finalState = evt
		case <-time.After(5 * time.Second):
			break LOOP
		}
	}

	require.Equal(t, h2.ID(), finalState.Peer)
	require.Equal(t, h1.network.Connectedness(h2.ID()), finalState.Connectedness)
}
