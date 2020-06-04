package ws

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pb "github.com/libp2p/go-libp2p-core/introspection/pb"
)

type simpleEvent struct {
	String string
	Number int
}

type EventA simpleEvent
type EventB simpleEvent
type EventC simpleEvent

func TestPushEvents(t *testing.T) {
	server, mocki := createTestServer(t)
	require.NoError(t, server.Start())
	defer server.Close()

	conn := createConn(t, server)
	defer conn.Close()

	conn.greet()
	conn.sendCommand(&pb.ClientCommand{Id: 201, Command: pb.ClientCommand_PUSH_ENABLE, Source: pb.ClientCommand_EVENTS})

	msg := conn.readNext()
	resp := msg.Payload.(*pb.ServerMessage_Response).Response

	require.EqualValues(t, 201, resp.Id)
	require.EqualValues(t, pb.CommandResponse_OK, resp.Result)

	assertEvent := func(name string) {
		msg := conn.readNext()
		evt := msg.Payload.(*pb.ServerMessage_Event).Event

		require.EqualValues(t, name, evt.Type.Name)
		require.NotEmpty(t, evt.Content)
		require.NotZero(t, evt.Ts)
	}

	mocki.EventCh<-EventA{String: "hello", Number: 100}
	mocki.EventCh<-EventA{String: "hello", Number: 100}
	mocki.EventCh<-EventB{String: "hello", Number: 100}

	assertEvent("EventA")
	assertEvent("EventA")
	assertEvent("EventB")
}

func TestPushStopPushEvents(t *testing.T) {
	server, mocki := createTestServer(t)
	require.NoError(t, server.Start())
	defer server.Close()

	conn := createConn(t, server)
	defer conn.Close()

	conn.greet()
	conn.sendCommand(&pb.ClientCommand{Id: 201, Command: pb.ClientCommand_PUSH_ENABLE, Source: pb.ClientCommand_EVENTS})

	msg := conn.readNext()
	resp := msg.Payload.(*pb.ServerMessage_Response).Response

	require.EqualValues(t, 201, resp.Id)
	require.EqualValues(t, pb.CommandResponse_OK, resp.Result)

	assertEvent := func(name string) {
		msg := conn.readNext()
		evt := msg.Payload.(*pb.ServerMessage_Event).Event

		require.EqualValues(t, name, evt.Type.Name)
		require.NotEmpty(t, evt.Content)
		require.NotZero(t, evt.Ts)
	}

	mocki.EventCh<-EventA{String: "hello", Number: 100}
	assertEvent("EventA")

	// now disable the pusher and verify that we actually missed those events.
	conn.sendCommand(&pb.ClientCommand{Id: 201, Command: pb.ClientCommand_PUSH_DISABLE, Source: pb.ClientCommand_EVENTS})
	msg = conn.readNext()
	resp = msg.Payload.(*pb.ServerMessage_Response).Response
	require.EqualValues(t, 201, resp.Id)
	require.EqualValues(t, pb.CommandResponse_OK, resp.Result)

	time.Sleep(1 * time.Second)

	// these events will be missed.
	mocki.EventCh<-EventA{String: "hello", Number: 100}
	mocki.EventCh<-EventB{String: "hello", Number: 100}

	// enable the pusher again
	conn.sendCommand(&pb.ClientCommand{Id: 201, Command: pb.ClientCommand_PUSH_ENABLE, Source: pb.ClientCommand_EVENTS})
	msg = conn.readNext()
	resp = msg.Payload.(*pb.ServerMessage_Response).Response
	require.EqualValues(t, 201, resp.Id)
	require.EqualValues(t, pb.CommandResponse_OK, resp.Result)

	// these events will be received.
	mocki.EventCh<-EventC{String: "hello", Number: 100}
	mocki.EventCh<-EventC{String: "hello", Number: 100}

	assertEvent("EventC")
	assertEvent("EventC")
}

func TestPushEventsIdempotent(t *testing.T) {
	server, _ := createTestServer(t)
	require.NoError(t, server.Start())
	defer server.Close()

	conn := createConn(t, server)
	defer conn.Close()

	conn.greet()
	conn.sendCommand(&pb.ClientCommand{Id: 201, Command: pb.ClientCommand_PUSH_ENABLE, Source: pb.ClientCommand_EVENTS})

	msg := conn.readNext()
	resp := msg.Payload.(*pb.ServerMessage_Response).Response

	require.EqualValues(t, 201, resp.Id)
	require.EqualValues(t, pb.CommandResponse_OK, resp.Result)

	conn.sendCommand(&pb.ClientCommand{Id: 202, Command: pb.ClientCommand_PUSH_ENABLE, Source: pb.ClientCommand_EVENTS})

	msg = conn.readNext()
	resp = msg.Payload.(*pb.ServerMessage_Response).Response

	require.EqualValues(t, 202, resp.Id)
	require.EqualValues(t, pb.CommandResponse_OK, resp.Result)
}
