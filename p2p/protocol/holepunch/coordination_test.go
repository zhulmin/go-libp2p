package holepunch_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch/pb"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/libp2p/go-msgio/protoio"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/stretchr/testify/require"
)

func TestDirectDialWorks(t *testing.T) {
	// all addrs should be marked as public
	cpy := manet.Private4
	manet.Private4 = []*net.IPNet{}
	defer func() {
		manet.Private4 = cpy
	}()

	ctx := context.Background()

	// try to hole punch without any connection and streams, if it works -> it's a direct connection
	h1, h1ps := mkHostWithHolePunchSvc(t, ctx)
	h2, _ := mkHostWithHolePunchSvc(t, ctx)
	h2.RemoveStreamHandler(holepunch.Protocol)
	h1.Peerstore().AddAddrs(h2.ID(), h2.Addrs(), peerstore.ConnectedAddrTTL)

	require.NoError(t, h1ps.HolePunch(h2.ID()))

	cs := h1.Network().ConnsToPeer(h2.ID())
	require.Len(t, cs, 1)

	cs = h2.Network().ConnsToPeer(h1.ID())
	require.Len(t, cs, 1)
}

func TestHolePunchFailuresOnInitiator(t *testing.T) {
	ctx := context.Background()

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
			errMsg: "expected HolePunch_CONNECT message",
		},
		"responder does NOT support protocol": {
			rhandler: nil,
			errMsg:   "protocol not supported",
		},
		"unable to READ CONNECT message from responder": {
			rhandler: func(s network.Stream) {
				s.Reset()
			},
			errMsg: "failed to read HolePunch_CONNECT message",
		},
		"responder does NOT reply within hole punch deadline": {
			holePunchTimeout: 10 * time.Millisecond,
			rhandler: func(s network.Stream) {
				for {

				}
			},
			errMsg: "i/o deadline reached",
		},
	}

	for name, tc := range tcs {
		t.Run(name, func(t *testing.T) {
			if tc.holePunchTimeout != 0 {
				cpy := holepunch.HolePunchTimeout
				holepunch.HolePunchTimeout = tc.holePunchTimeout
				defer func() {
					holepunch.HolePunchTimeout = cpy
				}()
			}

			h1, h1ps := mkHostWithHolePunchSvc(t, ctx)
			h2, _ := mkHostWithHolePunchSvc(t, ctx)

			if tc.rhandler != nil {
				h2.SetStreamHandler(holepunch.Protocol, tc.rhandler)
			} else {
				h2.RemoveStreamHandler(holepunch.Protocol)
			}

			connect(t, ctx, h1, h2)
			err := h1ps.HolePunch(h2.ID())
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errMsg)
		})

	}
}

func TestHolePunchFailuresOnResponder(t *testing.T) {

}

func connect(t *testing.T, ctx context.Context, h1, h2 host.Host) network.Conn {
	require.NoError(t, h1.Connect(ctx, peer.AddrInfo{
		ID:    h2.ID(),
		Addrs: h2.Addrs(),
	}))

	cs := h1.Network().ConnsToPeer(h2.ID())
	require.Len(t, cs, 1)
	return cs[0]
}

func mkHostWithHolePunchSvc(t *testing.T, ctx context.Context) (host.Host, *holepunch.HolePunchService) {
	h, err := libp2p.New(ctx)
	require.NoError(t, err)
	ids, err := identify.NewIDService(h)
	require.NoError(t, err)
	hps, err := holepunch.NewHolePunchService(h, ids)
	require.NoError(t, err)

	return h, hps
}
