package autonatv2

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	bhost "github.com/libp2p/go-libp2p/p2p/host/blank"
	"github.com/libp2p/go-libp2p/p2p/host/eventbus"
	swarmt "github.com/libp2p/go-libp2p/p2p/net/swarm/testing"
	"github.com/libp2p/go-libp2p/p2p/protocol/autonatv2/pbv2"

	"github.com/libp2p/go-msgio/pbio"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func newAutoNAT(t *testing.T, dialer host.Host, opts ...AutoNATOption) *AutoNAT {
	t.Helper()
	b := eventbus.NewBus()
	h := bhost.NewBlankHost(swarmt.GenSwarm(t, swarmt.EventBus(b)), bhost.WithEventBus(b))
	if dialer == nil {
		dialer = bhost.NewBlankHost(swarmt.GenSwarm(t))
	}
	an, err := New(h, dialer, opts...)
	if err != nil {
		t.Error(err)
	}
	an.srv.Enable()
	an.cli.Register()
	return an
}

func parseAddrs(t *testing.T, msg *pbv2.Message) []ma.Multiaddr {
	t.Helper()
	req := msg.GetDialRequest()
	addrs := make([]ma.Multiaddr, 0)
	for _, ab := range req.Addrs {
		a, err := ma.NewMultiaddrBytes(ab)
		if err != nil {
			t.Error("invalid addr bytes", ab)
		}
		addrs = append(addrs, a)
	}
	return addrs
}

func idAndConnect(t *testing.T, a, b host.Host) {
	a.Peerstore().AddAddrs(b.ID(), b.Addrs(), peerstore.PermanentAddrTTL)
	a.Peerstore().AddProtocols(b.ID(), DialProtocol)

	err := a.Connect(context.Background(), peer.AddrInfo{ID: b.ID()})
	require.NoError(t, err)
}

// waitForPeer waits for a to process all peer events
func waitForPeer(t *testing.T, a *AutoNAT) {
	t.Helper()
	require.Eventually(t, func() bool {
		a.mx.Lock()
		defer a.mx.Unlock()
		return len(a.peers) > 0
	}, 5*time.Second, 100*time.Millisecond)
}

// identify provides server address and protocol to client
func identify(t *testing.T, cli *AutoNAT, srv *AutoNAT) {
	idAndConnect(t, cli.host, srv.host)
	waitForPeer(t, cli)
}

func TestAutoNATPrivateAddr(t *testing.T) {
	an := newAutoNAT(t, nil)
	res, err := an.CheckReachability(context.Background(), []ma.Multiaddr{ma.StringCast("/ip4/192.168.0.1/udp/10/quic-v1")}, nil)
	require.Nil(t, res)
	require.NotNil(t, err)
}

func TestClientRequest(t *testing.T) {
	an := newAutoNAT(t, nil, allowAll)

	addrs := an.host.Addrs()

	var gotReq atomic.Bool
	p := bhost.NewBlankHost(swarmt.GenSwarm(t))
	p.SetStreamHandler(DialProtocol, func(s network.Stream) {
		gotReq.Store(true)
		r := pbio.NewDelimitedReader(s, maxMsgSize)
		var msg pbv2.Message
		if err := r.ReadMsg(&msg); err != nil {
			t.Error(err)
			return
		}
		if msg.GetDialRequest() == nil {
			t.Errorf("expected message to be of type DialRequest, got %T", msg.Msg)
			return
		}
		addrsb := make([][]byte, len(addrs))
		for i := 0; i < len(addrs); i++ {
			addrsb[i] = addrs[i].Bytes()
		}
		if !reflect.DeepEqual(addrsb, msg.GetDialRequest().Addrs) {
			t.Errorf("expected elements to be equal want: %s got: %s", addrsb, msg.GetDialRequest().Addrs)
		}
		s.Reset()
	})

	idAndConnect(t, an.host, p)
	waitForPeer(t, an)

	res, err := an.CheckReachability(context.Background(), addrs[:1], addrs[1:])
	require.Nil(t, res)
	require.NotNil(t, err)
	require.True(t, gotReq.Load())
}

func TestClientServerError(t *testing.T) {
	an := newAutoNAT(t, nil, allowAll)
	addrs := an.host.Addrs()

	p := bhost.NewBlankHost(swarmt.GenSwarm(t))
	idAndConnect(t, an.host, p)
	waitForPeer(t, an)

	done := make(chan bool)
	tests := []struct {
		handler func(network.Stream)
	}{
		{handler: func(s network.Stream) {
			s.Reset()
			done <- true
		}},
		{handler: func(s network.Stream) {
			r := pbio.NewDelimitedReader(s, maxMsgSize)
			var msg pbv2.Message
			r.ReadMsg(&msg)
			w := pbio.NewDelimitedWriter(s)
			w.WriteMsg(&pbv2.Message{
				Msg: &pbv2.Message_DialRequest{
					DialRequest: &pbv2.DialRequest{
						Addrs: [][]byte{},
						Nonce: 0,
					},
				},
			})
			if err := r.ReadMsg(&msg); err == nil {
				t.Errorf("expected read to fail: %T", msg.Msg)
			}
			done <- true
		}},
	}

	for i, tc := range tests {
		t.Run(fmt.Sprintf("test-%d", i), func(t *testing.T) {
			p.SetStreamHandler(DialProtocol, tc.handler)
			res, err := an.CheckReachability(context.Background(), addrs[:1], addrs[1:])
			require.Nil(t, res)
			require.NotNil(t, err)
			<-done
		})
	}
}

func TestClientDataRequest(t *testing.T) {
	an := newAutoNAT(t, nil, allowAll)
	addrs := an.host.Addrs()

	p := bhost.NewBlankHost(swarmt.GenSwarm(t))
	idAndConnect(t, an.host, p)
	waitForPeer(t, an)

	done := make(chan bool)
	tests := []struct {
		handler func(network.Stream)
	}{
		{handler: func(s network.Stream) {
			r := pbio.NewDelimitedReader(s, maxMsgSize)
			var msg pbv2.Message
			r.ReadMsg(&msg)
			w := pbio.NewDelimitedWriter(s)
			w.WriteMsg(&pbv2.Message{
				Msg: &pbv2.Message_DialDataRequest{
					DialDataRequest: &pbv2.DialDataRequest{
						AddrIdx:  0,
						NumBytes: 10000,
					},
				}},
			)
			remain := 10000
			for remain > 0 {
				if err := r.ReadMsg(&msg); err != nil {
					t.Errorf("expected a valid data response")
					break
				}
				if msg.GetDialDataResponse() == nil {
					t.Errorf("expected type DialDataResponse got %T", msg.Msg)
					break
				}
				remain -= len(msg.GetDialDataResponse().Data)
			}
			s.Reset()
			done <- true
		}},
		{handler: func(s network.Stream) {
			r := pbio.NewDelimitedReader(s, maxMsgSize)
			var msg pbv2.Message
			r.ReadMsg(&msg)
			w := pbio.NewDelimitedWriter(s)
			w.WriteMsg(&pbv2.Message{
				Msg: &pbv2.Message_DialDataRequest{
					DialDataRequest: &pbv2.DialDataRequest{
						AddrIdx:  1,
						NumBytes: 10000,
					},
				}},
			)
			if err := r.ReadMsg(&msg); err == nil {
				t.Errorf("expected to reject data request for low priority address")
			}
			s.Reset()
			done <- true
		}},
	}

	for i, tc := range tests {
		t.Run(fmt.Sprintf("test-%d", i), func(t *testing.T) {
			p.SetStreamHandler(DialProtocol, tc.handler)
			res, err := an.CheckReachability(context.Background(), addrs[:1], addrs[1:])
			require.Nil(t, res)
			require.NotNil(t, err)
			<-done
		})
	}
}

func TestClientDialAttempts(t *testing.T) {
	an := newAutoNAT(t, nil, allowAll)
	addrs := an.host.Addrs()

	p := bhost.NewBlankHost(swarmt.GenSwarm(t))
	idAndConnect(t, an.host, p)
	waitForPeer(t, an)

	tests := []struct {
		handler func(network.Stream)
		success bool
	}{
		{
			handler: func(s network.Stream) {
				r := pbio.NewDelimitedReader(s, maxMsgSize)
				var msg pbv2.Message
				if err := r.ReadMsg(&msg); err != nil {
					t.Error(err)
				}
				resp := &pbv2.DialResponse{
					Status:     pbv2.DialResponse_ResponseStatus_OK,
					DialStatus: pbv2.DialStatus_OK,
					AddrIdx:    0,
				}
				w := pbio.NewDelimitedWriter(s)
				w.WriteMsg(&pbv2.Message{
					Msg: &pbv2.Message_DialResponse{
						DialResponse: resp,
					},
				})
				s.Close()
			},
			success: false,
		},
		{
			handler: func(s network.Stream) {
				r := pbio.NewDelimitedReader(s, maxMsgSize)
				var msg pbv2.Message
				r.ReadMsg(&msg)
				req := msg.GetDialRequest()
				addrs := parseAddrs(t, &msg)
				hh := bhost.NewBlankHost(swarmt.GenSwarm(t))
				defer hh.Close()
				hh.Peerstore().AddAddr(s.Conn().RemotePeer(), addrs[1], peerstore.PermanentAddrTTL)
				as, err := hh.NewStream(context.Background(), s.Conn().RemotePeer(), AttemptProtocol)
				if err != nil {
					t.Error("failed to open stream", err)
					s.Reset()
					return
				}
				w := pbio.NewDelimitedWriter(as)
				w.WriteMsg(&pbv2.DialAttempt{Nonce: req.Nonce})
				as.CloseWrite()

				w = pbio.NewDelimitedWriter(s)
				w.WriteMsg(&pbv2.Message{
					Msg: &pbv2.Message_DialResponse{
						DialResponse: &pbv2.DialResponse{
							Status:     pbv2.DialResponse_ResponseStatus_OK,
							DialStatus: pbv2.DialStatus_OK,
							AddrIdx:    0,
						},
					},
				})
				s.Close()
			},
			success: false,
		},
		{
			handler: func(s network.Stream) {
				r := pbio.NewDelimitedReader(s, maxMsgSize)
				var msg pbv2.Message
				r.ReadMsg(&msg)
				req := msg.GetDialRequest()
				addrs := parseAddrs(t, &msg)
				hh := bhost.NewBlankHost(swarmt.GenSwarm(t))
				defer hh.Close()
				hh.Peerstore().AddAddr(s.Conn().RemotePeer(), addrs[1], peerstore.PermanentAddrTTL)
				as, err := hh.NewStream(context.Background(), s.Conn().RemotePeer(), AttemptProtocol)
				as.SetDeadline(time.Now().Add(5 * time.Second))
				if err != nil {
					t.Error("failed to open stream", err)
					s.Reset()
					return
				}
				ww := pbio.NewDelimitedWriter(as)
				if err := ww.WriteMsg(&pbv2.DialAttempt{Nonce: req.Nonce - 1}); err != nil {
					s.Reset()
					as.Reset()
					return
				}
				as.CloseWrite()
				defer func() {
					data := make([]byte, 1)
					as.Read(data)
					as.Close()
				}()

				w := pbio.NewDelimitedWriter(s)
				w.WriteMsg(&pbv2.Message{
					Msg: &pbv2.Message_DialResponse{
						DialResponse: &pbv2.DialResponse{
							Status:     pbv2.DialResponse_ResponseStatus_OK,
							DialStatus: pbv2.DialStatus_OK,
							AddrIdx:    0,
						},
					},
				})
				s.Close()
			},
			success: false,
		},
		{
			handler: func(s network.Stream) {
				r := pbio.NewDelimitedReader(s, maxMsgSize)
				var msg pbv2.Message
				r.ReadMsg(&msg)
				req := msg.GetDialRequest()
				addrs := parseAddrs(t, &msg)

				hh := bhost.NewBlankHost(swarmt.GenSwarm(t))
				defer hh.Close()
				hh.Peerstore().AddAddr(s.Conn().RemotePeer(), addrs[1], peerstore.PermanentAddrTTL)
				as, err := hh.NewStream(context.Background(), s.Conn().RemotePeer(), AttemptProtocol)
				if err != nil {
					t.Error("failed to open stream", err)
					s.Reset()
					return
				}

				w := pbio.NewDelimitedWriter(as)
				if err := w.WriteMsg(&pbv2.DialAttempt{Nonce: req.Nonce}); err != nil {
					t.Error("failed to write nonce", err)
					s.Reset()
					as.Reset()
					return
				}
				as.CloseWrite()
				defer func() {
					data := make([]byte, 1)
					as.Read(data)
					as.Close()
				}()

				w = pbio.NewDelimitedWriter(s)

				w.WriteMsg(&pbv2.Message{
					Msg: &pbv2.Message_DialResponse{
						DialResponse: &pbv2.DialResponse{
							Status:     pbv2.DialResponse_ResponseStatus_OK,
							DialStatus: pbv2.DialStatus_OK,
							AddrIdx:    1,
						},
					},
				})
				s.Close()
			},
			success: true,
		},
	}

	for i, tc := range tests {
		t.Run(fmt.Sprintf("test-%d", i), func(t *testing.T) {
			p.SetStreamHandler(DialProtocol, tc.handler)
			res, err := an.CheckReachability(context.Background(), addrs[:1], addrs[1:])
			require.NoError(t, err)
			if !tc.success {
				require.Equal(t, res.Reachability, network.ReachabilityUnknown)
				require.NotEqual(t, res.Status, pbv2.DialStatus_OK, "got: %d", res.Status)
			} else {
				require.Equal(t, res.Reachability, network.ReachabilityPublic)
				require.Equal(t, res.Status, pbv2.DialStatus_OK)
			}
		})
	}
}

func TestEventSubscription(t *testing.T) {
	an := newAutoNAT(t, nil)
	defer an.host.Close()

	b := bhost.NewBlankHost(swarmt.GenSwarm(t))
	defer b.Close()
	c := bhost.NewBlankHost(swarmt.GenSwarm(t))
	defer c.Close()

	idAndConnect(t, an.host, b)
	require.Eventually(t, func() bool {
		an.mx.Lock()
		defer an.mx.Unlock()
		return len(an.peers) == 1
	}, 5*time.Second, 100*time.Millisecond)

	idAndConnect(t, an.host, c)
	require.Eventually(t, func() bool {
		an.mx.Lock()
		defer an.mx.Unlock()
		return len(an.peers) == 2
	}, 5*time.Second, 100*time.Millisecond)

	an.host.Network().ClosePeer(b.ID())
	require.Eventually(t, func() bool {
		an.mx.Lock()
		defer an.mx.Unlock()
		return len(an.peers) == 1
	}, 5*time.Second, 100*time.Millisecond)

	an.host.Network().ClosePeer(c.ID())
	require.Eventually(t, func() bool {
		an.mx.Lock()
		defer an.mx.Unlock()
		return len(an.peers) == 0
	}, 5*time.Second, 100*time.Millisecond)
}
