package transport_integration

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func TestStreamDeadlines(t *testing.T) {
	for _, tc := range transportsToTest {
		t.Run(tc.Name, func(t *testing.T) {
			if strings.Contains(tc.Name, "WebSocket") || strings.Contains(tc.Name, "Yamux") {
				t.Skip("Yamux does not adhere to deadlines: https://github.com/libp2p/go-yamux/issues/104")
			}
			if strings.Contains(tc.Name, "mplex") {
				t.Skip("In a localhost test, writes may succeed instantly so a select { <-ctx.Done; <-write } may write.")
			}

			listener := tc.HostGenerator(t, TransportTestCaseOpts{})
			dialer := tc.HostGenerator(t, TransportTestCaseOpts{NoListen: true})

			require.NoError(t, dialer.Connect(context.Background(), peer.AddrInfo{ID: listener.ID(), Addrs: listener.Addrs()}))

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			// Use cancelled context
			_, err := dialer.Network().NewStream(ctx, listener.ID())
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestDialDeadlines(t *testing.T) {
	portMatcher := regexp.MustCompile(`(tcp|udp)\/\d+`)
	for _, tc := range transportsToTest {
		t.Run(tc.Name, func(t *testing.T) {
			listener := tc.HostGenerator(t, TransportTestCaseOpts{})
			dialer := tc.HostGenerator(t, TransportTestCaseOpts{NoListen: true})

			listenerAddrString := listener.Addrs()[0].String()
			// Replace the actual port with one that won't work
			listenerAddr := multiaddr.StringCast(portMatcher.ReplaceAllString(listenerAddrString, "$1/2"))

			dialer.Peerstore().AddAddr(listener.ID(), listenerAddr, peerstore.PermanentAddrTTL)

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			// Use cancelled context
			_, err := dialer.Network().DialPeer(ctx, listener.ID())
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}
