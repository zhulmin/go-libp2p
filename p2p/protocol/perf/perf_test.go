package perf

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

func TestPerf(t *testing.T) {
	h1, err := libp2p.New()
	require.NoError(t, err)
	h2, err := libp2p.New()
	require.NoError(t, err)

	perf1 := NewPerfService(h1)
	_ = NewPerfService(h2)

	err = h1.Connect(context.Background(), peer.AddrInfo{
		ID:    h2.ID(),
		Addrs: h2.Addrs(),
	})
	require.NoError(t, err)

	start := time.Now()

	// Warmup
	err = perf1.RunPerf(context.Background(), h2.ID(), 10<<20, 10<<20)
	require.NoError(t, err)

	err = perf1.RunPerf(context.Background(), h2.ID(), 0, 10<<20)
	require.NoError(t, err)
	fmt.Println("10 MiB download took: ", time.Since(start))

	err = perf1.RunPerf(context.Background(), h2.ID(), 10<<20, 0<<20)
	require.NoError(t, err)
	fmt.Println("10 MiB upload took: ", time.Since(start))

}
