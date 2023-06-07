package transport_integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

func TestNewStreamDeadlines(t *testing.T) {
	for _, tc := range transportsToTest {
		t.Run(tc.Name, func(t *testing.T) {
			if strings.Contains(tc.Name, "WebSocket") || strings.Contains(tc.Name, "Yamux") {
				t.Skip("Yamux does not adhere to deadlines: https://github.com/libp2p/go-yamux/issues/104")
			}
			if strings.Contains(tc.Name, "mplex") {
				t.Skip("In a localhost test, writes may succeed instantly so a select { <-ctx.Done; <-write } may write.")
			}

			listener := tc.HostGenerator(t, TransportTestCaseOpts{})
			dialer := tc.HostGenerator(t, TransportTestCaseOpts{NoListen: true})

			require.NoError(t, dialer.Connect(context.Background(), peer.AddrInfo{ID: listener.ID(), Addrs: listener.Addrs()}))

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			// Use cancelled context
			_, err := dialer.Network().NewStream(ctx, listener.ID())
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestReadWriteDeadlines(t *testing.T) {
	// Send a lot of data so that writes have to flush (can't just buffer it all)
	sendBuf := make([]byte, 1<<20)

	for _, tc := range transportsToTest {
		t.Run(tc.Name, func(t *testing.T) {
			if strings.Contains(tc.Name, "mplex") {
				t.Skip("Fixme: mplex fails this test")
				return
			}
			listener := tc.HostGenerator(t, TransportTestCaseOpts{})
			dialer := tc.HostGenerator(t, TransportTestCaseOpts{NoListen: true})

			require.NoError(t, dialer.Connect(context.Background(), peer.AddrInfo{
				ID:    listener.ID(),
				Addrs: listener.Addrs(),
			}))

			// This simply stalls
			listener.SetStreamHandler("/stall", func(s network.Stream) {
				time.Sleep(time.Hour)
				s.Close()
			})

			t.Run("ReadDeadline", func(t *testing.T) {
				s, err := dialer.NewStream(context.Background(), listener.ID(), "/stall")
				require.NoError(t, err)
				defer s.Close()

				start := time.Now()
				// Set a deadline
				s.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
				buf := make([]byte, 1)
				_, err = s.Read(buf)
				require.Error(t, err)
				require.Contains(t, err.Error(), "deadline")
				require.Less(t, time.Since(start), 100*time.Millisecond)

				if strings.Contains(tc.Name, "mplex") {
					// FIXME: mplex stalls on close, so we reset so we don't spend an extra 5s waiting for nothing
					s.Reset()
				}
			})

			t.Run("WriteDeadline", func(t *testing.T) {
				s, err := dialer.NewStream(context.Background(), listener.ID(), "/stall")
				require.NoError(t, err)
				defer s.Close()

				// Set a deadline
				s.SetWriteDeadline(time.Now().Add(10 * time.Millisecond))
				start := time.Now()
				_, err = s.Write(sendBuf)
				require.Error(t, err)
				require.Contains(t, err.Error(), "deadline")
				require.Less(t, time.Since(start), 100*time.Millisecond)

				if strings.Contains(tc.Name, "mplex") {
					// FIXME: mplex stalls on close, so we reset so we don't spend an extra 5s waiting for nothing
					s.Reset()
				}
			})

			// Like the above, but with SetDeadline
			t.Run("SetDeadline", func(t *testing.T) {
				for _, op := range []string{"Read", "Write"} {
					t.Run(op, func(t *testing.T) {
						s, err := dialer.NewStream(context.Background(), listener.ID(), "/stall")
						require.NoError(t, err)
						defer s.Close()

						// Set a deadline
						s.SetDeadline(time.Now().Add(10 * time.Millisecond))
						start := time.Now()

						if op == "Read" {
							buf := make([]byte, 1)
							_, err = s.Read(buf)
						} else {
							_, err = s.Write(sendBuf)
						}
						require.Error(t, err)
						require.Contains(t, err.Error(), "deadline")
						require.Less(t, time.Since(start), 100*time.Millisecond)

						if strings.Contains(tc.Name, "mplex") {
							// FIXME: mplex stalls on close, so we reset so we don't spend an extra 5s waiting for nothing
							s.Reset()
						}
					})
				}
			})
		})
	}
}
