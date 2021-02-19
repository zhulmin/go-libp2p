package holepunch_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	circuit "github.com/libp2p/go-libp2p-circuit"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"
	holepunch_pb "github.com/libp2p/go-libp2p/p2p/protocol/holepunch/pb"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/libp2p/go-msgio/protoio"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/stretchr/testify/require"
)

type mockEventTracer struct {
	errs []string
}

func (m *mockEventTracer) Trace(evt *holepunch.Event) {
	if evt.Type != holepunch.ProtocolErrorEvtT {
		return
	}
	m.errs = append(m.errs, evt.Evt.(*holepunch.ProtocolErrorEvt).Error)
}

var _ holepunch.EventTracer = &mockEventTracer{}

func TestDirectDialWorks(t *testing.T) {
	// all addrs should be marked as public
	cpy := manet.Private4
	manet.Private4 = []*net.IPNet{}
	defer func() { manet.Private4 = cpy }()

	h1, h1ps := mkHostWithHolePunchSvc(t)
	h2, _ := mkHostWithHolePunchSvc(t)
	h2.RemoveStreamHandler(holepunch.Protocol)
	h1.Peerstore().AddAddrs(h2.ID(), h2.Addrs(), peerstore.ConnectedAddrTTL)

	// try to hole punch without any connection and streams, if it works -> it's a direct connection
	require.Len(t, h1.Network().ConnsToPeer(h2.ID()), 0)
	require.NoError(t, h1ps.HolePunch(h2.ID()))
	require.GreaterOrEqual(t, len(h1.Network().ConnsToPeer(h2.ID())), 1)
	require.GreaterOrEqual(t, len(h2.Network().ConnsToPeer(h1.ID())), 1)
}

func TestEndToEndSimConnect(t *testing.T) {
	// all addrs should be marked as public
	cpy := manet.Private4
	manet.Private4 = []*net.IPNet{}
	defer func() { manet.Private4 = cpy }()

	r := mkRelay(t, context.Background())
	h1, _ := mkHostWithHolePunchSvc(t)
	h2, _ := mkHostWithStaticAutoRelay(t, context.Background(), r)

	// h1 has a relay addr
	// h2 should connect to the relay addr
	var raddr ma.Multiaddr
	for _, a := range h2.Addrs() {
		if _, err := a.ValueForProtocol(ma.P_CIRCUIT); err == nil {
			raddr = a
			break
		}
	}
	require.NotEmpty(t, raddr)
	require.NoError(t, h1.Connect(context.Background(), peer.AddrInfo{
		ID:    h2.ID(),
		Addrs: []ma.Multiaddr{raddr},
	}))

	// wait till a direct connection is complete
	ensureDirectConn(t, h1, h2)
	// ensure no hole-punching streams are open on either side
	ensureNoHolePunchingStream(t, h1, h2)
}

func TestFailuresOnInitiator(t *testing.T) {
	t.Skip("broken test")

	tcs := map[string]struct {
		rhandler         func(s network.Stream)
		errMsg           string
		holePunchTimeout time.Duration
	}{
		"responder does NOT send a CONNECT message": {
			rhandler: func(s network.Stream) {
				wr := protoio.NewDelimitedWriter(s)
				msg := new(holepunch_pb.HolePunch)
				msg.Type = holepunch_pb.HolePunch_SYNC.Enum()
				wr.WriteMsg(msg)
			},
			errMsg: "expected CONNECT message",
		},
		"responder does NOT support protocol": {
			rhandler: nil,
			errMsg:   "protocol not supported",
		},
		"unable to READ CONNECT message from responder": {
			rhandler: func(s network.Stream) {
				s.Reset()
			},
			errMsg: "failed to read CONNECT message",
		},
		"responder does NOT reply within hole punch deadline": {
			holePunchTimeout: 10 * time.Millisecond,
			rhandler: func(s network.Stream) {
				time.Sleep(10 * time.Second)
			},
			errMsg: "i/o deadline reached",
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			if tc.holePunchTimeout != 0 {
				cpy := holepunch.StreamTimeout
				holepunch.StreamTimeout = tc.holePunchTimeout
				defer func() {
					holepunch.StreamTimeout = cpy
				}()
			}

			h1, h1ps := mkHostWithHolePunchSvc(t)
			h2, _ := mkHostWithHolePunchSvc(t)

			if tc.rhandler != nil {
				h2.SetStreamHandler(holepunch.Protocol, tc.rhandler)
			} else {
				h2.RemoveStreamHandler(holepunch.Protocol)
			}

			connect(t, context.Background(), h1, h2)
			err := h1ps.HolePunch(h2.ID())
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errMsg)
		})

	}
}

func TestFailuresOnResponder(t *testing.T) {
	tcs := map[string]struct {
		initiator        func(s network.Stream)
		errMsg           string
		holePunchTimeout time.Duration
	}{
		"initiator does NOT send a CONNECT message": {
			initiator: func(s network.Stream) {
				protoio.NewDelimitedWriter(s).WriteMsg(&holepunch_pb.HolePunch{Type: holepunch_pb.HolePunch_SYNC.Enum()})
			},
			errMsg: "expected CONNECT message",
		},
		"initiator does NOT send a SYNC message after a Connect message": {
			initiator: func(s network.Stream) {
				w := protoio.NewDelimitedWriter(s)
				w.WriteMsg(&holepunch_pb.HolePunch{Type: holepunch_pb.HolePunch_CONNECT.Enum()})
				w.WriteMsg(&holepunch_pb.HolePunch{Type: holepunch_pb.HolePunch_CONNECT.Enum()})
			},
			errMsg: "expected SYNC message",
		},
		"initiator does NOT reply within hole punch deadline": {
			holePunchTimeout: 10 * time.Millisecond,
			initiator: func(s network.Stream) {
				protoio.NewDelimitedWriter(s).WriteMsg(&holepunch_pb.HolePunch{Type: holepunch_pb.HolePunch_CONNECT.Enum()})
				time.Sleep(10 * time.Second)
			},
			errMsg: "i/o deadline reached",
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			if tc.holePunchTimeout != 0 {
				cpy := holepunch.StreamTimeout
				holepunch.StreamTimeout = tc.holePunchTimeout
				defer func() { holepunch.StreamTimeout = cpy }()
			}

			tr := &mockEventTracer{}
			h1, _ := mkHostWithHolePunchSvc(t)
			h2, _ := mkHostWithHolePunchSvc(t, holepunch.WithTracer(tr))
			connect(t, context.Background(), h1, h2)

			s, err := h1.NewStream(context.Background(), h2.ID(), holepunch.Protocol)
			require.NoError(t, err)

			go tc.initiator(s)

			require.Eventually(t, func() bool { return len(tr.errs) > 0 }, 5*time.Second, 100*time.Millisecond)
			require.Len(t, tr.errs, 1)
			require.Contains(t, tr.errs[0], tc.errMsg)
		})

	}
}

func ensureNoHolePunchingStream(t *testing.T, h1, h2 host.Host) {
	require.Eventually(t, func() bool {
		for _, c := range h1.Network().ConnsToPeer(h2.ID()) {
			for _, s := range c.GetStreams() {
				if s.ID() == string(holepunch.Protocol) {
					return false
				}
			}
		}
		return true
	}, 5*time.Second, 50*time.Millisecond)

	require.Eventually(t, func() bool {
		for _, c := range h2.Network().ConnsToPeer(h1.ID()) {
			for _, s := range c.GetStreams() {
				if s.ID() == string(holepunch.Protocol) {
					return false
				}
			}
		}
		return true
	}, 5*time.Second, 50*time.Millisecond)
}

func ensureDirectConn(t *testing.T, h1, h2 host.Host) {
	require.Eventually(t, func() bool {
		for _, c := range h1.Network().ConnsToPeer(h2.ID()) {
			if _, err := c.RemoteMultiaddr().ValueForProtocol(ma.P_CIRCUIT); err != nil {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond)

	require.Eventually(t, func() bool {
		for _, c := range h2.Network().ConnsToPeer(h1.ID()) {
			if _, err := c.RemoteMultiaddr().ValueForProtocol(ma.P_CIRCUIT); err != nil {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond)
}

func connect(t *testing.T, ctx context.Context, h1, h2 host.Host) {
	require.NoError(t, h1.Connect(ctx, peer.AddrInfo{
		ID:    h2.ID(),
		Addrs: h2.Addrs(),
	}))
	require.GreaterOrEqual(t, len(h1.Network().ConnsToPeer(h2.ID())), 1)
}

func mkHostWithStaticAutoRelay(t *testing.T, ctx context.Context, relay host.Host) (host.Host, *holepunch.HolePunchService) {
	pi := peer.AddrInfo{
		ID:    relay.ID(),
		Addrs: relay.Addrs(),
	}

	h, err := libp2p.New(ctx,
		libp2p.ListenAddrs(ma.StringCast("/ip4/127.0.0.1/tcp/0"), ma.StringCast("/ip6/::1/tcp/0")),
		libp2p.EnableRelay(),
		libp2p.EnableAutoRelay(),
		libp2p.ForceReachabilityPrivate(),
		libp2p.StaticRelays([]peer.AddrInfo{pi}),
	)
	require.NoError(t, err)
	ids, err := identify.NewIDService(h)
	require.NoError(t, err)
	hps, err := holepunch.NewHolePunchService(h, ids)
	require.NoError(t, err)

	// wait till we have a relay addr
	require.Eventually(t, func() bool {
		for _, a := range h.Addrs() {
			if _, err := a.ValueForProtocol(ma.P_CIRCUIT); err == nil {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond)

	return h, hps
}

func mkRelay(t *testing.T, ctx context.Context) host.Host {
	h, err := libp2p.New(ctx,
		libp2p.ListenAddrs(ma.StringCast("/ip4/127.0.0.1/tcp/0"), ma.StringCast("/ip6/::1/tcp/0")),
		libp2p.EnableRelay(circuit.OptHop),
	)
	require.NoError(t, err)
	return h
}

func mkHostWithHolePunchSvc(t *testing.T, opts ...holepunch.Option) (host.Host, *holepunch.HolePunchService) {
	h, err := libp2p.New(
		context.Background(),
		libp2p.ListenAddrs(ma.StringCast("/ip4/127.0.0.1/tcp/0"), ma.StringCast("/ip6/::1/tcp/0")),
	)
	require.NoError(t, err)
	ids, err := identify.NewIDService(h)
	require.NoError(t, err)
	hps, err := holepunch.NewHolePunchService(h, ids, opts...)
	require.NoError(t, err)
	return h, hps
}
