package libp2p

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p-core/host"
	introspection "github.com/libp2p/go-libp2p-core/introspection"
	introspection_pb "github.com/libp2p/go-libp2p-core/introspection/pb"
	"github.com/libp2p/go-libp2p-core/metrics"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	introspector "github.com/libp2p/go-libp2p-introspector"

	"github.com/gogo/protobuf/proto"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

var (
	msg1 = []byte("1")
	msg2 = []byte("12")
	msg3 = []byte("111")
	msg4 = []byte("0000")

	p1 = protocol.ID("h1h3")
	p2 = protocol.ID("h2h1")
)

// TODO Send Pause & Send Data
func TestIntrospector(t *testing.T) {
	require := require.New(t)

	iaddr := "127.0.0.1:0"
	ctx := context.Background()

	// create host 1 with introspector
	h1, err := New(ctx,
		Introspector(
			introspector.NewDefaultIntrospector(),
			introspector.WsServerWithConfig(&introspector.WsServerConfig{
				ListenAddrs: []string{iaddr},
			}),
		),
		BandwidthReporter(metrics.NewBandwidthCounter()),
	)
	require.NoError(err)
	defer h1.Close()

	// create host 2
	h2, err := New(ctx)
	defer h2.Close()

	// create host 3
	h3, err := New(ctx)
	defer h3.Close()

	// host1 -> CONNECTS -> host2
	require.NoError(h1.Connect(ctx, peer.AddrInfo{ID: h2.ID(), Addrs: h2.Addrs()}))

	// host3 -> CONNECTS -> host1
	require.NoError(h3.Connect(ctx, peer.AddrInfo{ID: h1.ID(), Addrs: h1.Addrs()}))

	// host1 -> OPENS STREAM 1 -> host3, Writes a message & then reads the response
	h3.SetStreamHandler(p1, func(s network.Stream) {
		buf := make([]byte, len(msg1))

		_, err := io.ReadFull(s, buf)
		require.NoError(err)

		_, err = s.Write(msg2)
		require.NoError(err)
	})

	s1, err := h1.NewStream(ctx, h3.ID(), p1)
	require.NoError(err)

	_, err = s1.Write(msg1)
	require.NoError(err)

	buf := make([]byte, len(msg2))
	_, err = io.ReadFull(s1, buf)
	require.NoError(err)

	// host2 -> OPENS Stream 2 -> host1 , writes a message & reads the response
	h1.SetStreamHandler(p2, func(s network.Stream) {
		buf := make([]byte, len(msg3))

		_, err := io.ReadFull(s, buf)
		require.NoError(err)

		_, err = s.Write(msg4)
		require.NoError(err)
	})

	s2, err := h2.NewStream(ctx, h1.ID(), p2)
	require.NoError(err)

	_, err = s2.Write(msg3)
	require.NoError(err)

	buf = make([]byte, len(msg4))
	_, err = io.ReadFull(s2, buf)
	require.NoError(err)

	// create a connection with the introspection server
	addrs := h1.(host.IntrospectableHost).IntrospectionEndpoint().ListenAddrs()
	url := fmt.Sprintf("ws://%s/introspect", addrs[0])

	fmt.Println(addrs)

	// wait till connection is established
	var connection *websocket.Conn
	require.Eventually(func() bool {
		connection, _, err = websocket.DefaultDialer.Dial(url, nil)
		return err == nil
	}, 5*time.Second, 500*time.Millisecond)
	defer connection.Close()

	// first, we get the runtime and assert it
	pd := fetchProtocolWrapper(require, connection)
	rt := pd.GetRuntime()
	require.NotNil(t, rt)
	require.Equal(h1.ID().String(), rt.PeerId)
	require.Equal(runtime.GOOS, rt.Platform)
	require.Equal("go-libp2p", rt.Implementation)

	// followed by the state message
	pd = fetchProtocolWrapper(require, connection)
	st := pd.GetState()
	require.NotNil(t, st)
	assertState(require, st, h1, h2, h3, false)

	// we should then periodically get a state message..lets; wait for one with traffic
	var st2 *introspection_pb.State
	require.Eventually(func() bool {
		pd = fetchProtocolWrapper(require, connection)
		st2 = pd.GetState()
		require.NotNil(t, st2)

		if st2.Traffic.TrafficOut.CumBytes != 0 &&
			st2.Subsystems.Connections[0].Traffic.TrafficOut.CumBytes != 0 && st2.Subsystems.Connections[1].Traffic.TrafficOut.CumBytes != 0 {
			return true
		}

		return false
	}, 10*time.Second, 1500*time.Millisecond)
	assertState(require, st2, h1, h2, h3, true)

	// Pause
	cl := &introspection_pb.ClientSignal{Signal: introspection_pb.ClientSignal_PAUSE_PUSH_EMITTER}
	bz, err := proto.Marshal(cl)
	require.NoError(err)
	require.NotEmpty(bz)
	require.NoError(connection.WriteMessage(websocket.BinaryMessage, bz))
	time.Sleep(1 * time.Second)

	// We get a state message when we unpause
	cl = &introspection_pb.ClientSignal{Signal: introspection_pb.ClientSignal_UNPAUSE_PUSH_EMITTER}
	bz, err = proto.Marshal(cl)
	require.NoError(err)
	require.NotEmpty(bz)
	require.NoError(connection.WriteMessage(websocket.BinaryMessage, bz))
	time.Sleep(1 * time.Second)

	pd = fetchProtocolWrapper(require, connection)
	st = pd.GetState()
	require.NotNil(t, st)
	assertState(require, st, h1, h2, h3, true)
}

func assertState(require *require.Assertions, state *introspection_pb.State, h1, h2, h3 host.Host,
	assertTraffic bool) {

	if assertTraffic {
		require.Greater(state.Traffic.TrafficIn.CumBytes, uint64(100))
		require.Greater(state.Traffic.TrafficOut.CumBytes, uint64(100))
	}

	// Connections
	conns := state.Subsystems.Connections
	peerIdToConns := make(map[string]*introspection_pb.Connection)
	for _, c := range conns {
		peerIdToConns[c.PeerId] = c
	}
	require.Len(peerIdToConns, 2)

	pconn := make(map[string]network.Conn)
	for _, c := range h1.Network().Conns() {
		pconn[c.RemotePeer().String()] = c
	}
	require.Len(pconn, 2)

	// host1 -> host2 connection
	h2Conn := peerIdToConns[h2.ID().String()]
	require.NotEmpty(h2Conn.Id)
	require.Equal(introspection_pb.Status_ACTIVE, h2Conn.Status)
	require.Equal(pconn[h2.ID().String()].LocalMultiaddr().String(), h2Conn.Endpoints.SrcMultiaddr)
	require.Equal(pconn[h2.ID().String()].RemoteMultiaddr().String(), h2Conn.Endpoints.DstMultiaddr)
	require.Equal(introspection_pb.Role_INITIATOR, h2Conn.Role)

	if assertTraffic {
		require.Greater(h2Conn.Traffic.TrafficIn.CumBytes, uint64(len(msg3)))
	}

	// host3 -> host1 connection
	h3Conn := peerIdToConns[h3.ID().String()]
	require.NotEmpty(h3Conn.Id)
	require.Equal(introspection_pb.Status_ACTIVE, h3Conn.Status)
	require.Equal(pconn[h3.ID().String()].LocalMultiaddr().String(), h3Conn.Endpoints.SrcMultiaddr)
	require.Equal(pconn[h3.ID().String()].RemoteMultiaddr().String(), h3Conn.Endpoints.DstMultiaddr)
	require.Equal(introspection_pb.Role_RESPONDER, h3Conn.Role)

	if assertTraffic {
		require.Greater(h3Conn.Traffic.TrafficIn.CumBytes, uint64(len(msg2)))
		require.Greater(h3Conn.Traffic.TrafficOut.CumBytes, uint64(len(msg1)))
	}
	// stream1
	require.Len(h3Conn.Streams.Streams, 1)
	h3Stream := h3Conn.Streams.Streams[0]
	require.NotEmpty(h3Stream.Id)
	require.Equal(string(p1), h3Stream.Protocol)
	require.Equal(introspection_pb.Role_INITIATOR, h3Stream.Role)
	require.Equal(introspection_pb.Status_ACTIVE, h3Stream.Status)
	// require.True(len(msg1) == int(h3Stream.Traffic.TrafficOut.CumBytes))
	// require.True(len(msg2) == int(h3Stream.Traffic.TrafficIn.CumBytes))

	// stream 2
	require.Len(h2Conn.Streams.Streams, 1)
	h1Stream := h2Conn.Streams.Streams[0]
	require.NotEmpty(h1Stream.Id)
	require.Equal(string(p2), h1Stream.Protocol)
	require.Equal(introspection_pb.Role_RESPONDER, h1Stream.Role)
	require.Equal(introspection_pb.Status_ACTIVE, h1Stream.Status)
	// require.True(len(msg3) == int(h1Stream.Traffic.TrafficIn.CumBytes))
}

func fetchProtocolWrapper(require *require.Assertions, conn *websocket.Conn) *introspection_pb.ProtocolDataPacket {
	_, msg, err := conn.ReadMessage()
	require.NoError(err)
	require.NotEmpty(msg)
	pd := &introspection_pb.ProtocolDataPacket{}
	require.NoError(proto.Unmarshal(msg, pd))
	require.NotNil(pd.Message)
	require.Equal(introspection.ProtoVersion, pd.Version.Version)
	return pd
}
