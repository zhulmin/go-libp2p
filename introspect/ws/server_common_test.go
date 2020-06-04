package ws

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"testing"

	"github.com/benbjohnson/clock"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/libp2p/go-libp2p-core/introspection"
	pb "github.com/libp2p/go-libp2p-core/introspection/pb"
	"github.com/libp2p/go-libp2p/introspect"
)

func createTestServer(t *testing.T) (*Server, *introspect.MockIntrospector) {
	t.Helper()

	mocki := introspect.NewMockIntrospector()
	cfg := &ServerConfig{ListenAddrs: []string{"localhost:0"}, Clock: clock.NewMock()}
	server, err := NewServer(mocki, cfg)
	require.NoError(t, err)
	return server, mocki
}

type connWrapper struct {
	*websocket.Conn
	t *testing.T
}

func createConn(t *testing.T, server *Server) *connWrapper {
	addr := fmt.Sprintf("ws://%s/introspect", server.ListenAddrs()[0])
	conn, _, err := websocket.DefaultDialer.Dial(addr, nil)
	require.NoError(t, err)
	return &connWrapper{conn, t}
}

func (cw *connWrapper) sendCommand(cmd *pb.ClientCommand) {
	cw.t.Helper()

	msg, err := cmd.Marshal()
	require.NoError(cw.t, err)

	err = cw.WriteMessage(websocket.BinaryMessage, msg)
	require.NoError(cw.t, err)
}

func (cw *connWrapper) greet() {
	cw.t.Helper()

	cw.sendCommand(&pb.ClientCommand{Id: 200, Command: pb.ClientCommand_HELLO})

	msg := cw.readNext()
	resp := msg.Payload.(*pb.ServerMessage_Response).Response

	require.EqualValues(cw.t, 200, resp.Id)
	require.EqualValues(cw.t, pb.CommandResponse_OK, resp.Result)
	require.Empty(cw.t, resp.Error)
}

func (cw *connWrapper) readNext() *pb.ServerMessage {
	cw.t.Helper()

	_, msg, err := cw.ReadMessage()
	require.NoError(cw.t, err)

	var (
		// parse the message
		version  = msg[0:4]
		checksum = msg[4:8]
		length   = msg[8:12]
		payload  = msg[12:]
	)

	require.EqualValues(cw.t, introspection.ProtoVersion, binary.LittleEndian.Uint32(version))
	require.EqualValues(cw.t, len(payload), binary.LittleEndian.Uint32(length))

	// validate hash.
	h := fnv.New32a()
	_, err = h.Write(payload)
	require.NoError(cw.t, err)
	require.EqualValues(cw.t, h.Sum32(), binary.LittleEndian.Uint32(checksum))

	smsg := &pb.ServerMessage{}

	// read the protocol message directly
	require.NoError(cw.t, smsg.Unmarshal(payload))

	require.NotNil(cw.t, smsg.Payload, "nil message received from server")
	require.Equal(cw.t, introspection.ProtoVersion, smsg.Version.Version, "incorrect proto version receieved from client")

	return smsg
}
