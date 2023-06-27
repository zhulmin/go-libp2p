package network

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	ma "github.com/multiformats/go-multiaddr"

	"github.com/stretchr/testify/require"
)

func TestDedupAddrs(t *testing.T) {
	tcpAddr := ma.StringCast("/ip4/127.0.0.1/tcp/1234")
	quicAddr := ma.StringCast("/ip4/127.0.0.1/udp/1234/quic-v1")
	wsAddr := ma.StringCast("/ip4/127.0.0.1/tcp/1234/ws")

	type testcase struct {
		in, out []ma.Multiaddr
	}

	for i, tc := range []testcase{
		{in: nil, out: nil},
		{in: []ma.Multiaddr{tcpAddr}, out: []ma.Multiaddr{tcpAddr}},
		{in: []ma.Multiaddr{tcpAddr, tcpAddr, tcpAddr}, out: []ma.Multiaddr{tcpAddr}},
		{in: []ma.Multiaddr{tcpAddr, quicAddr, tcpAddr}, out: []ma.Multiaddr{tcpAddr, quicAddr}},
		{in: []ma.Multiaddr{tcpAddr, quicAddr, wsAddr}, out: []ma.Multiaddr{tcpAddr, quicAddr, wsAddr}},
	} {
		tc := tc
		t.Run(fmt.Sprintf("test %d", i), func(t *testing.T) {
			deduped := DedupAddrs(tc.in)
			for _, a := range tc.out {
				require.Contains(t, deduped, a)
			}
		})
	}
}

func BenchmarkDedupAddrs(b *testing.B) {
	b.ReportAllocs()
	var addrs []ma.Multiaddr
	r := rand.New(rand.NewSource(1234))
	for i := 0; i < 100; i++ {
		tcpAddr := ma.StringCast(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", r.Intn(math.MaxUint16)))
		quicAddr := ma.StringCast(fmt.Sprintf("/ip4/127.0.0.1/udp/%d/quic-v1", r.Intn(math.MaxUint16)))
		wsAddr := ma.StringCast(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/ws", r.Intn(math.MaxUint16)))
		addrs = append(addrs, tcpAddr, tcpAddr, quicAddr, quicAddr, wsAddr)
	}
	for _, sz := range []int{10, 20, 30, 50, 100} {
		b.Run(fmt.Sprintf("%d", sz), func(b *testing.B) {
			items := make([]ma.Multiaddr, sz)
			for i := 0; i < b.N; i++ {
				copy(items, addrs[:sz])
				DedupAddrs(items)
			}
		})
	}
}
