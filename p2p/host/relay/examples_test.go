package relay_test

import (
	"context"

	libp2p "github.com/libp2p/go-libp2p"

	host "github.com/libp2p/go-libp2p-host"
	routing "github.com/libp2p/go-libp2p-routing"
)

func ExampleNewRelayHost() {
	ctx := context.Background()

	libp2p.New(ctx, libp2p.EnableRelay())
}

func ExampleNewAutoRelayHost() {
	ctx := context.Background()

	// In a non-example use case `makeRouting` will need to return an instance of
	// the DHT, using https://godoc.org/github.com/libp2p/go-libp2p-kad-dht#New
	makeRouting := func(h host.Host) (routing.PeerRouting, error) {
		mtab := newMockRoutingTable()
		mr := newMockRouting(h, mtab)
		return mr, nil
	}

	opts := []libp2p.Option{libp2p.EnableRelay(), libp2p.EnableAutoRelay(), libp2p.Routing(makeRouting)}

	libp2p.New(ctx, opts...)
}
