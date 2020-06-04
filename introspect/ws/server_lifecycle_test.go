package ws

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServerClose(t *testing.T) {
	server, _ := createTestServer(t)
	require.NoError(t, server.Start())

	// create 100 connections.
	var wg sync.WaitGroup
	conns := make([]*connWrapper, 100)
	wg.Add(100)
	for i := range conns {
		conn := createConn(t, server)
		go func() {
			_, _, err := conn.ReadMessage()
			if strings.Contains(err.Error(), io.ErrUnexpectedEOF.Error()) {
				wg.Done()
			}
		}()
		conns[i] = conn
	}

	err := server.Close()
	require.NoError(t, err)
	wg.Wait()
}

func TestServerHandlesClosedConns(t *testing.T) {
	server, _ := createTestServer(t)
	require.NoError(t, server.Start())

	conns := make([]*connWrapper, 100)
	for i := range conns {
		conn := createConn(t, server)
		conns[i] = conn
	}

	require.Len(t, server.Sessions(), 100)

	err := conns[0].Close()
	require.NoError(t, err)

	require.Eventually(t, func() bool { return len(server.Sessions()) == 99 }, 2*time.Second, 100*time.Millisecond)
}
