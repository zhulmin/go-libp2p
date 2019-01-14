package relay

import (
	"context"

	"github.com/libp2p/go-libp2p"
)

// godoc examples

func ExampleNewRelayHost() {
	ctx := context.Background()

	libp2p.New(ctx, libp2p.EnableRelay())
}