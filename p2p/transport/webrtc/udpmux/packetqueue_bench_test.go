package udpmux

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"testing"

	pool "github.com/libp2p/go-buffer-pool"
)

var sizes = []int{
	1,
	10,
	100,
	maxPacketsInQueue,
}

func BenchmarkQueue(b *testing.B) {
	addr := net.UDPAddrFromAddrPort(netip.MustParseAddrPort("127.0.0.1:12345"))
	for _, dequeue := range []bool{true, false} {
		for _, input := range sizes {
			testCase := fmt.Sprintf("enqueue_%d", input)
			if dequeue {
				testCase = testCase + "_dequeue"
			}
			b.Run(testCase, func(b *testing.B) {
				pq := newPacketQueue()
				buf := make([]byte, 256)
				for i := 0; i < b.N; i++ {
					for k := 0; k < input; k++ {
						pq.Push(pool.Get(255), addr)
					}
					for k := 0; k < input; k++ {

						pq.Pop(context.Background(), buf)
					}
				}
			})
		}
	}
}
