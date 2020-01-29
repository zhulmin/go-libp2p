package libp2p

import (
	"context"
	"github.com/golang/protobuf/proto"
	"github.com/gorilla/websocket"
	"github.com/libp2p/go-libp2p-core/introspect"
	"github.com/libp2p/go-libp2p-core/metrics"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/libp2p/go-libp2p-introspection/introspection"
	"github.com/stretchr/testify/require"
	"net/url"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestIntrospector(t *testing.T) {
	msg1 := []byte("1")
	msg2 := []byte("12")
	msg3 := []byte("111")
	msg4 := []byte("0000")

	iaddr := "0.0.0.0:9999"
	ctx := context.Background()

	// create host 1 with introspector
	h1, err := New(ctx, Introspector(introspection.NewDefaultIntrospector(iaddr)), BandwidthReporter(metrics.NewBandwidthCounter()))
	require.NoError(t, err)
	defer h1.Close()

	// create host 2
	h2, err := New(ctx)
	defer h2.Close()

	// create host 3
	h3, err := New(ctx)
	defer h3.Close()

	// host1 -> CONNECTS -> host2
	require.NoError(t, h1.Connect(ctx, peer.AddrInfo{h2.ID(), h2.Addrs()}))

	// host3 -> CONNECTS -> host1
	require.NoError(t, h3.Connect(ctx, peer.AddrInfo{h1.ID(), h1.Addrs()}))

	// host1 -> OPENS STREAM 1 -> host3, Writes a message & then reads the response
	var wg sync.WaitGroup
	p1 := protocol.ID("h1h3")
	h3.SetStreamHandler(p1, func(s network.Stream) {
		bz := make([]byte, len(msg1))
		_, err := s.Read(bz)
		require.NoError(t, err)
		_, err = s.Write(msg2)
		require.NoError(t, err)
		wg.Done()
	})
	s1, err := h1.NewStream(ctx, h3.ID(), p1)
	require.NoError(t, err)
	wg.Add(1)
	_, err = s1.Write(msg1)
	require.NoError(t, err)
	bz1 := make([]byte, len(msg2))
	wg.Wait()
	_, err = s1.Read(bz1)
	require.NoError(t, err)

	// host2 -> OPENS Stream 2 -> host1 , writes a message & reads the response
	p2 := protocol.ID("h2h1")
	h1.SetStreamHandler(p2, func(s network.Stream) {
		bz := make([]byte, len(msg3))
		_, err := s.Read(bz)
		require.NoError(t, err)
		_, err = s.Write(msg4)
		require.NoError(t, err)
		wg.Done()
	})

	s2, err := h2.NewStream(ctx, h1.ID(), p2)
	require.NoError(t, err)
	wg.Add(1)
	_, err = s2.Write(msg3)
	require.NoError(t, err)
	bz2 := make([]byte, len(msg4))
	wg.Wait()
	_, err = s2.Read(bz2)
	require.NoError(t, err)

	// call introspection server & fetch state
	u := url.URL{Scheme: "ws", Host: iaddr, Path: "/introspect"}

	// wait till connection is established
	i := 0
	var connection *websocket.Conn
	for {
		require.Less(t, i, 5, "failed to start server even after 5 attempts")
		connection, _, err = websocket.DefaultDialer.Dial(u.String(), nil)
		if err == nil {
			break
		}
		i++
		time.Sleep(500 * time.Millisecond)
	}
	defer connection.Close()

	// fetch & unmarshal h1 state till ALL BANDWIDTH METERES kick in
	var state *introspect.State
	for {
		require.NoError(t, connection.WriteMessage(websocket.TextMessage, []byte("trigger fetch")))
		// read snapshot
		_, msg, err := connection.ReadMessage()
		require.NoError(t, err)
		require.NotEmpty(t, msg)

		state = &introspect.State{}
		require.NoError(t, proto.Unmarshal(msg, state))
		if state.Traffic.TrafficOut.CumBytes != 0 &&
			state.Subsystems.Connections[0].Traffic.TrafficOut.CumBytes != 0 && state.Subsystems.Connections[1].Traffic.TrafficOut.CumBytes != 0 {
			break
		}
	}

	// Assert State

	// Version
	require.Equal(t, introspect.ProtoVersion, state.Version.Number)

	// Runtime
	require.Equal(t, h1.ID().String(), state.Runtime.PeerId)
	require.Equal(t, runtime.GOOS, state.Runtime.Platform)
	require.Equal(t, "go-libp2p", state.Runtime.Implementation)

	// Overall Traffic
	require.Greater(t, state.Traffic.TrafficIn.CumBytes, uint64(100))
	require.Greater(t, state.Traffic.TrafficOut.CumBytes, uint64(100))

	// Connections
	conns := state.Subsystems.Connections
	peerIdToConns := make(map[string]*introspect.Connection)
	for _, c := range conns {
		peerIdToConns[c.PeerId] = c
	}
	require.Len(t, peerIdToConns, 2)

	pconn := make(map[string]network.Conn)
	for _, c := range h1.Network().Conns() {
		pconn[c.RemotePeer().String()] = c
	}
	require.Len(t, pconn, 2)

	// host1 -> host2 connection
	h2Conn := peerIdToConns[h2.ID().String()]
	require.NotEmpty(t, h2Conn.Id)
	require.Equal(t, introspect.Status_ACTIVE, h2Conn.Status)
	require.Equal(t, pconn[h2.ID().String()].LocalMultiaddr().String(), h2Conn.Endpoints.SrcMultiaddr)
	require.Equal(t, pconn[h2.ID().String()].RemoteMultiaddr().String(), h2Conn.Endpoints.DstMultiaddr)
	require.Equal(t, introspect.Role_INITIATOR, h2Conn.Role)
	require.Equal(t, uint64(len(msg3)), h2Conn.Traffic.TrafficIn.CumBytes)

	// host3 -> host1 connection
	h3Conn := peerIdToConns[h3.ID().String()]
	require.NotEmpty(t, h3Conn.Id)
	require.Equal(t, introspect.Status_ACTIVE, h3Conn.Status)
	require.Equal(t, pconn[h3.ID().String()].LocalMultiaddr().String(), h3Conn.Endpoints.SrcMultiaddr)
	require.Equal(t, pconn[h3.ID().String()].RemoteMultiaddr().String(), h3Conn.Endpoints.DstMultiaddr)
	require.Equal(t, introspect.Role_RESPONDER, h3Conn.Role)
	require.Equal(t, uint64(len(msg2)), h3Conn.Traffic.TrafficIn.CumBytes)
	require.Equal(t, uint64(len(msg1)), h3Conn.Traffic.TrafficOut.CumBytes)

	// stream1
	require.Len(t, h3Conn.Streams.Streams, 1)
	h3Stream := h3Conn.Streams.Streams[0]
	require.NotEmpty(t, h3Stream.Id)
	require.Equal(t, string(p1), h3Stream.Protocol)
	require.Equal(t, introspect.Role_INITIATOR, h3Stream.Role)
	require.Equal(t, introspect.Status_ACTIVE, h3Stream.Status)
	require.True(t, len(msg1) == int(h3Stream.Traffic.TrafficOut.CumBytes))
	require.True(t, len(msg2) == int(h3Stream.Traffic.TrafficIn.CumBytes))

	// stream 2
	require.Len(t, h2Conn.Streams.Streams, 1)
	h1Stream := h2Conn.Streams.Streams[0]
	require.NotEmpty(t, h1Stream.Id)
	require.Equal(t, string(p2), h1Stream.Protocol)
	require.Equal(t, introspect.Role_RESPONDER, h1Stream.Role)
	require.Equal(t, introspect.Status_ACTIVE, h1Stream.Status)
	require.True(t, len(msg3) == int(h1Stream.Traffic.TrafficIn.CumBytes))
}
