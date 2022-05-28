package autorelay_test

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	relayv1 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv1/relay"
	circuitv2_proto "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/proto"
	relayv2 "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"

	"github.com/benbjohnson/clock"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func numRelays(h host.Host) int {
	peers := make(map[peer.ID]struct{})
	for _, addr := range h.Addrs() {
		addr, comp := ma.SplitLast(addr)
		if comp.Protocol().Code != ma.P_CIRCUIT { // not a relay addr
			continue
		}
		_, comp = ma.SplitLast(addr)
		if comp.Protocol().Code != ma.P_P2P {
			panic("expected p2p component")
		}
		id, err := peer.Decode(comp.Value())
		if err != nil {
			panic(err)
		}
		peers[id] = struct{}{}
	}
	return len(peers)
}

func newPrivateNode(t *testing.T, opts ...autorelay.Option) host.Host {
	t.Helper()
	h, err := libp2p.New(
		libp2p.ForceReachabilityPrivate(),
		libp2p.EnableAutoRelay(opts...),
	)
	require.NoError(t, err)
	return h
}

func newRelay(t *testing.T) host.Host {
	t.Helper()
	h, err := libp2p.New(
		libp2p.DisableRelay(),
		libp2p.EnableRelayService(),
		libp2p.ForceReachabilityPublic(),
		libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
			for i, addr := range addrs {
				saddr := addr.String()
				if strings.HasPrefix(saddr, "/ip4/127.0.0.1/") {
					addrNoIP := strings.TrimPrefix(saddr, "/ip4/127.0.0.1")
					addrs[i] = ma.StringCast("/dns4/localhost" + addrNoIP)
				}
			}
			return addrs
		}),
	)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		for _, p := range h.Mux().Protocols() {
			if p == circuitv2_proto.ProtoIDv2Hop {
				return true
			}
		}
		return false
	}, 500*time.Millisecond, 10*time.Millisecond)
	return h
}

func newRelayV1(t *testing.T) host.Host {
	t.Helper()
	h, err := libp2p.New(
		libp2p.DisableRelay(),
		libp2p.ForceReachabilityPublic(),
		libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
			for i, addr := range addrs {
				saddr := addr.String()
				if strings.HasPrefix(saddr, "/ip4/127.0.0.1/") {
					addrNoIP := strings.TrimPrefix(saddr, "/ip4/127.0.0.1")
					addrs[i] = ma.StringCast("/dns4/localhost" + addrNoIP)
				}
			}
			return addrs
		}),
	)
	require.NoError(t, err)
	r, err := relayv1.NewRelay(h)
	require.NoError(t, err)
	t.Cleanup(func() { r.Close() })
	return h
}

// creates a node that speaks the relay v2 protocol, but doesn't accept any reservations for the first workAfter tries
func newBrokenRelay(t *testing.T, workAfter int) host.Host {
	t.Helper()
	h, err := libp2p.New(
		libp2p.DisableRelay(),
		libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
			for i, addr := range addrs {
				saddr := addr.String()
				if strings.HasPrefix(saddr, "/ip4/127.0.0.1/") {
					addrNoIP := strings.TrimPrefix(saddr, "/ip4/127.0.0.1")
					addrs[i] = ma.StringCast("/dns4/localhost" + addrNoIP)
				}
			}
			return addrs
		}),
		libp2p.EnableRelayService(),
	)
	require.NoError(t, err)
	var n int32
	h.SetStreamHandler(circuitv2_proto.ProtoIDv2Hop, func(str network.Stream) {
		str.Reset()
		num := atomic.AddInt32(&n, 1)
		if int(num) >= workAfter {
			h.RemoveStreamHandler(circuitv2_proto.ProtoIDv2Hop)
			r, err := relayv2.New(h)
			require.NoError(t, err)
			t.Cleanup(func() { r.Close() })
		}
	})
	return h
}

func TestSingleCandidate(t *testing.T) {
	var counter int
	h := newPrivateNode(t,
		autorelay.WithPeerSource(func(num int) <-chan peer.AddrInfo {
			counter++
			require.Equal(t, 1, num)
			peerChan := make(chan peer.AddrInfo, num)
			defer close(peerChan)
			r := newRelay(t)
			t.Cleanup(func() { r.Close() })
			peerChan <- peer.AddrInfo{ID: r.ID(), Addrs: r.Addrs()}
			return peerChan
		}),
		autorelay.WithMaxCandidates(1),
		autorelay.WithNumRelays(99999),
		autorelay.WithBootDelay(0),
	)
	defer h.Close()

	require.Eventually(t, func() bool { return numRelays(h) > 0 }, 3*time.Second, 100*time.Millisecond)
	// test that we don't add any more relays
	require.Never(t, func() bool { return numRelays(h) > 1 }, 200*time.Millisecond, 50*time.Millisecond)
	require.Equal(t, 1, counter, "expected the peer source callback to only have been called once")
}

func TestSingleRelay(t *testing.T) {
	const numCandidates = 3
	var counter int
	h := newPrivateNode(t,
		autorelay.WithPeerSource(func(num int) <-chan peer.AddrInfo {
			counter++
			require.Equal(t, numCandidates, num)
			peerChan := make(chan peer.AddrInfo, num)
			defer close(peerChan)
			for i := 0; i < num; i++ {
				r := newRelay(t)
				t.Cleanup(func() { r.Close() })
				peerChan <- peer.AddrInfo{ID: r.ID(), Addrs: r.Addrs()}
			}
			return peerChan
		}),
		autorelay.WithMaxCandidates(numCandidates),
		autorelay.WithNumRelays(1),
		autorelay.WithBootDelay(0),
	)
	defer h.Close()

	require.Eventually(t, func() bool { return numRelays(h) > 0 }, 5*time.Second, 100*time.Millisecond)
	// test that we don't add any more relays
	require.Never(t, func() bool { return numRelays(h) > 1 }, 200*time.Millisecond, 50*time.Millisecond)
	require.Equal(t, 1, counter, "expected the peer source callback to only have been called once")
}
func TestPreferRelayV2(t *testing.T) {
	r := newRelay(t)
	defer r.Close()
	// The relay supports both v1 and v2. The v1 stream handler should never be called,
	// if we prefer v2 relays.
	r.SetStreamHandler(relayv1.ProtoID, func(str network.Stream) {
		str.Reset()
		t.Fatal("used relay v1")
	})

	h := newPrivateNode(t,
		autorelay.WithPeerSource(func(int) <-chan peer.AddrInfo {
			peerChan := make(chan peer.AddrInfo, 1)
			defer close(peerChan)
			peerChan <- peer.AddrInfo{ID: r.ID(), Addrs: r.Addrs()}
			return peerChan
		}),
		autorelay.WithMaxCandidates(1),
		autorelay.WithNumRelays(99999),
		autorelay.WithBootDelay(0),
	)
	defer h.Close()

	require.Eventually(t, func() bool { return numRelays(h) > 0 }, 3*time.Second, 100*time.Millisecond)
}

func TestWaitForCandidates(t *testing.T) {
	peerChan := make(chan peer.AddrInfo)
	h := newPrivateNode(t,
		autorelay.WithPeerSource(func(int) <-chan peer.AddrInfo { return peerChan }),
		autorelay.WithMinCandidates(2),
		autorelay.WithNumRelays(1),
		autorelay.WithBootDelay(time.Hour),
	)
	defer h.Close()

	r1 := newRelay(t)
	t.Cleanup(func() { r1.Close() })
	peerChan <- peer.AddrInfo{ID: r1.ID(), Addrs: r1.Addrs()}

	// make sure we don't add any relays yet
	// We need to wait until we have at least 2 candidates before we connect.
	require.Never(t, func() bool { return numRelays(h) > 0 }, 200*time.Millisecond, 50*time.Millisecond)

	r2 := newRelay(t)
	t.Cleanup(func() { r2.Close() })
	peerChan <- peer.AddrInfo{ID: r2.ID(), Addrs: r2.Addrs()}
	require.Eventually(t, func() bool { return numRelays(h) > 0 }, 3*time.Second, 100*time.Millisecond)
}

func TestMaxCandidateAge(t *testing.T) {
	const numCandidates = 3
	var counter int32 // to be used atomically
	cl := clock.NewMock()
	h := newPrivateNode(t,
		autorelay.WithPeerSource(func(num int) <-chan peer.AddrInfo {
			atomic.AddInt32(&counter, 1)
			require.Equal(t, numCandidates, num)
			peerChan := make(chan peer.AddrInfo, num)
			defer close(peerChan)
			for i := 0; i < num; i++ {
				r := newRelay(t)
				t.Cleanup(func() { r.Close() })
				peerChan <- peer.AddrInfo{ID: r.ID(), Addrs: r.Addrs()}
			}
			return peerChan
		}),
		autorelay.WithMaxCandidates(numCandidates),
		autorelay.WithNumRelays(1),
		autorelay.WithMaxCandidateAge(time.Hour),
		autorelay.WithBootDelay(0),
		autorelay.WithClock(cl),
	)
	defer h.Close()

	r1 := newRelay(t)
	t.Cleanup(func() { r1.Close() })

	require.Eventually(t, func() bool { return numRelays(h) == 1 }, 5*time.Second, 50*time.Millisecond)
	require.Equal(t, 1, int(atomic.LoadInt32(&counter)))
	cl.Add(40 * time.Minute)
	require.Never(t, func() bool { return numRelays(h) > 1 }, 100*time.Millisecond, 25*time.Millisecond)
	cl.Add(30 * time.Minute)
	require.Eventually(t, func() bool { return atomic.LoadInt32(&counter) == 2 }, time.Second, 25*time.Millisecond)
}

func TestBackoff(t *testing.T) {
	const backoff = 10 * time.Second
	peerChan := make(chan peer.AddrInfo)
	cl := clock.NewMock()
	h := newPrivateNode(t,
		autorelay.WithPeerSource(func(int) <-chan peer.AddrInfo { return peerChan }),
		autorelay.WithNumRelays(1),
		autorelay.WithBootDelay(0),
		autorelay.WithBackoff(backoff),
		autorelay.WithClock(cl),
	)
	defer h.Close()

	r1 := newBrokenRelay(t, 1)
	t.Cleanup(func() { r1.Close() })
	peerChan <- peer.AddrInfo{ID: r1.ID(), Addrs: r1.Addrs()}

	// make sure we don't add any relays yet
	require.Never(t, func() bool { return numRelays(h) > 0 }, 100*time.Millisecond, 20*time.Millisecond)
	cl.Add(backoff * 2 / 3)
	require.Never(t, func() bool { return numRelays(h) > 0 }, 100*time.Millisecond, 20*time.Millisecond)
	cl.Add(backoff * 2 / 3)
	require.Eventually(t, func() bool { return numRelays(h) > 0 }, 500*time.Millisecond, 10*time.Millisecond)
}

func TestMaxBackoffs(t *testing.T) {
	const backoff = 20 * time.Second
	cl := clock.NewMock()
	peerChan := make(chan peer.AddrInfo)
	h := newPrivateNode(t,
		autorelay.WithPeerSource(func(int) <-chan peer.AddrInfo { return peerChan }),
		autorelay.WithNumRelays(1),
		autorelay.WithBootDelay(0),
		autorelay.WithBackoff(backoff),
		autorelay.WithMaxAttempts(3),
		autorelay.WithClock(cl),
	)
	defer h.Close()

	r := newBrokenRelay(t, 4)
	t.Cleanup(func() { r.Close() })
	peerChan <- peer.AddrInfo{ID: r.ID(), Addrs: r.Addrs()}

	// make sure we don't add any relays yet
	for i := 0; i < 5; i++ {
		cl.Add(backoff * 3 / 2)
		require.Never(t, func() bool { return numRelays(h) > 0 }, 50*time.Millisecond, 20*time.Millisecond)
	}
}

func TestStaticRelays(t *testing.T) {
	const numStaticRelays = 3
	var staticRelays []peer.AddrInfo
	for i := 0; i < numStaticRelays; i++ {
		r := newRelay(t)
		t.Cleanup(func() { r.Close() })
		staticRelays = append(staticRelays, peer.AddrInfo{ID: r.ID(), Addrs: r.Addrs()})
	}

	h := newPrivateNode(t,
		autorelay.WithStaticRelays(staticRelays),
		autorelay.WithNumRelays(1),
	)
	defer h.Close()

	require.Eventually(t, func() bool { return numRelays(h) > 0 }, 2*time.Second, 50*time.Millisecond)
}

func TestRelayV1(t *testing.T) {
	t.Run("relay v1 support disabled", func(t *testing.T) {
		peerChan := make(chan peer.AddrInfo)
		go func() {
			r := newRelayV1(t)
			peerChan <- peer.AddrInfo{ID: r.ID(), Addrs: r.Addrs()}
			t.Cleanup(func() { r.Close() })
		}()
		h := newPrivateNode(t,
			autorelay.WithPeerSource(func(int) <-chan peer.AddrInfo { return peerChan }),
			autorelay.WithBootDelay(0),
		)
		defer h.Close()

		require.Never(t, func() bool { return numRelays(h) > 0 }, 250*time.Millisecond, 100*time.Millisecond)
	})

	t.Run("relay v1 support enabled", func(t *testing.T) {
		peerChan := make(chan peer.AddrInfo)
		go func() {
			r := newRelayV1(t)
			peerChan <- peer.AddrInfo{ID: r.ID(), Addrs: r.Addrs()}
			t.Cleanup(func() { r.Close() })
		}()
		h := newPrivateNode(t,
			autorelay.WithPeerSource(func(int) <-chan peer.AddrInfo { return peerChan }),
			autorelay.WithBootDelay(0),
			autorelay.WithCircuitV1Support(),
		)
		defer h.Close()

		require.Eventually(t, func() bool { return numRelays(h) > 0 }, 3*time.Second, 100*time.Millisecond)
	})
}
