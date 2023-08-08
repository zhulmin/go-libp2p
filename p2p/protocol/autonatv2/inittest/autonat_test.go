package autonat_test

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/network"
	basichost "github.com/libp2p/go-libp2p/p2p/host/basic"
	"github.com/libp2p/go-libp2p/p2p/protocol/autonatv2"
	"github.com/stretchr/testify/require"
)

func TestAutoNATInit(t *testing.T) {
	h, err := libp2p.New()
	if err != nil {
		t.Fatal(err)
	}
	bh := h.(*basichost.BasicHost)
	require.NotNil(t, bh.GetAutoNatV2())

	h, err = libp2p.New(libp2p.DisableAutoNATv2())
	if err != nil {
		t.Fatal(err)
	}
	bh = h.(*basichost.BasicHost)
	require.Nil(t, bh.GetAutoNatV2())
}

func TestAutoNATReachabilityUpdate(t *testing.T) {
	h, err := libp2p.New(libp2p.ForceReachabilityPublic())
	if err != nil {
		t.Fatal(err)
	}
	bh := h.(*basichost.BasicHost)
	require.Eventually(t, func() bool {
		for _, p := range bh.Mux().Protocols() {
			if p == autonatv2.DialProtocol {
				return true
			}
		}
		return false
	}, 5*time.Second, 100*time.Millisecond)

	emitter, err := h.EventBus().Emitter(new(event.EvtLocalReachabilityChanged))
	require.NoError(t, err)
	emitter.Emit(event.EvtLocalReachabilityChanged{Reachability: network.ReachabilityPrivate})
	require.Eventually(t, func() bool {
		for _, p := range bh.Mux().Protocols() {
			if p == autonatv2.DialProtocol {
				return false
			}
		}
		return true
	}, 5*time.Second, 100*time.Millisecond)

	emitter.Emit(event.EvtLocalReachabilityChanged{Reachability: network.ReachabilityPublic})
	require.Eventually(t, func() bool {
		for _, p := range bh.Mux().Protocols() {
			if p == autonatv2.DialProtocol {
				return true
			}
		}
		return false
	}, 5*time.Second, 100*time.Millisecond)
}
