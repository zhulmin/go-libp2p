package webrtc_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2pwebrtc "github.com/libp2p/go-libp2p/p2p/transport/webrtc"
	"github.com/stretchr/testify/require"
)

func TestWebRTCStream(t *testing.T) {
	h1, err := libp2p.New(
		libp2p.Transport(libp2pwebrtc.New),
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/webrtc"),
	)
	require.NoError(t, err)

	type WriteResult struct {
		data string
		err  error
	}
	writeResultCh := make(chan WriteResult, 1)

	const proto = "/testing"
	h1.SetStreamHandler(proto, func(str network.Stream) {
		data, err := io.ReadAll(str)
		writeResultCh <- WriteResult{string(data), err}
	})

	h2, err := libp2p.New(
		libp2p.Transport(libp2pwebrtc.New),
		libp2p.NoListenAddrs,
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = h2.Connect(ctx, peer.AddrInfo{ID: h1.ID(), Addrs: h1.Addrs()})
	require.NoError(t, err)

	str, err := h2.NewStream(ctx, h1.ID(), proto)
	require.NoError(t, err)
	defer str.Close()

	_, err = str.Write([]byte("foobar"))
	require.NoError(t, err)

	var writeResult WriteResult
	select {
	case writeResult = <-writeResultCh:
		// ok
	case <-time.After(10 * time.Second):
		t.Fatal("timeout")
	}
	require.NoError(t, writeResult.err)
	require.Equal(t, "foobar", writeResult.data)
}
