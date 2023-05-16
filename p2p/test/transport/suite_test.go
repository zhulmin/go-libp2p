package transport_integration

import (
	"regexp"
	"testing"

	"github.com/libp2p/go-libp2p/core/transport"
	ttransport "github.com/libp2p/go-libp2p/p2p/transport/testsuite"
	libp2pwebtransport "github.com/libp2p/go-libp2p/p2p/transport/webtransport"
	ma "github.com/multiformats/go-multiaddr"
	multiaddr "github.com/multiformats/go-multiaddr"
)

func TestWithSuite(t *testing.T) {
	portMatcher := regexp.MustCompile(`(tcp|udp)\/\d+`)

	for _, tc := range transportsToTest {
		t.Run(tc.Name, func(t *testing.T) {
			// We use this host just to get a valid multiaddr for this transport.
			hostToGetAddr := tc.HostGenerator(t, TransportTestCaseOpts{NoRcmgr: true})
			a := multiaddr.StringCast(portMatcher.ReplaceAllString(hostToGetAddr.Addrs()[0].String(), "$1/0"))
			hostToGetAddr.Close()

			if tc.Name == "WebTransport" {
				// Remove certhash components
				_, n := libp2pwebtransport.IsWebtransportMultiaddr(a)
				for i := 0; i < n; i++ {
					a, _ = multiaddr.SplitLast(a)
				}
			}

			listener := tc.HostGenerator(t, TransportTestCaseOpts{NoRcmgr: true, NoListen: true, DisableQuicReuseport: false}) // Transport suite will listen on its own
			defer listener.Close()
			dialer := tc.HostGenerator(t, TransportTestCaseOpts{NoRcmgr: true, NoListen: true, DisableQuicReuseport: true})
			defer dialer.Close()

			type transportForListeninger interface {
				TransportForListening(a ma.Multiaddr) transport.Transport
			}

			listenerTransport := listener.Network().(transportForListeninger).TransportForListening(a)
			dialerTransport := dialer.Network().(transportForListeninger).TransportForListening(a)
			ttransport.SubtestTransport(t, listenerTransport, dialerTransport, a.String(), listener.ID())
		})
	}
}
