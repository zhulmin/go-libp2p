package autonatv2

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	bhost "github.com/libp2p/go-libp2p/p2p/host/blank"
	swarmt "github.com/libp2p/go-libp2p/p2p/net/swarm/testing"
	"github.com/libp2p/go-libp2p/p2p/protocol/autonatv2/pbv2"

	"github.com/libp2p/go-msgio/pbio"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func newAutoNAT(t *testing.T, dialer host.Host, opts ...AutoNATOption) *AutoNAT {
	t.Helper()
	h := bhost.NewBlankHost(swarmt.GenSwarm(t))
	if dialer == nil {
		dialer = bhost.NewBlankHost(swarmt.GenSwarm(t))
	}
	an, err := New(h, dialer, opts...)
	if err != nil {
		t.Error(err)
	}
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

func TestValidPeer(t *testing.T) {
	an := newAutoNAT(t, nil)
	require.Equal(t, an.validPeer(), peer.ID(""))
	an.host.Peerstore().AddAddr("peer1", ma.StringCast("/ip4/127.0.0.1/tcp/1"), peerstore.PermanentAddrTTL)
	an.host.Peerstore().AddAddr("peer2", ma.StringCast("/ip4/127.0.0.1/tcp/2"), peerstore.PermanentAddrTTL)
	require.NoError(t, an.host.Peerstore().AddProtocols("peer1", DialProtocol))
	require.NoError(t, an.host.Peerstore().AddProtocols("peer2", DialProtocol))

	var got1, got2 bool
	for i := 0; i < 100; i++ {
		p := an.validPeer()
		switch p {
		case peer.ID("peer1"):
			got1 = true
		case peer.ID("peer2"):
			got2 = true
		default:
			t.Fatalf("invalid peer: %s", p)
		}
		if got1 && got2 {
			break
		}
	}
	require.True(t, got1)
	require.True(t, got2)
}

func TestAutoNATPrivateAddr(t *testing.T) {
	an := newAutoNAT(t, nil)
	res, err := an.CheckReachability(context.Background(), []ma.Multiaddr{ma.StringCast("/ip4/192.168.0.1/udp/10/quic-v1")}, nil)
	require.Nil(t, res)
	require.NotNil(t, err)
}

func TestClientRequest(t *testing.T) {
	an := newAutoNAT(t, nil)

	addrs := an.host.Addrs()

	p := bhost.NewBlankHost(swarmt.GenSwarm(t))
	p.SetStreamHandler(DialProtocol, func(s network.Stream) {
		r := pbio.NewDelimitedReader(s, maxMsgSize)
		var msg pbv2.Message
		err := r.ReadMsg(&msg)
		if err != nil {
			t.Error(err)
		}
		require.NotNil(t, msg.GetDialRequest())
		addrsb := make([][]byte, len(addrs))
		for i := 0; i < len(addrs); i++ {
			addrsb[i] = addrs[i].Bytes()
		}
		if !reflect.DeepEqual(addrsb, msg.GetDialRequest().Addrs) {
			t.Errorf("expected elements to be equal want: %s got: %s", addrsb, msg.GetDialRequest().Addrs)
		}
		s.Reset()
	})

	an.host.Peerstore().AddAddrs(p.ID(), p.Addrs(), peerstore.TempAddrTTL)
	an.host.Peerstore().AddProtocols(p.ID(), DialProtocol)
	res, err := an.CheckReachability(context.Background(), addrs[:1], addrs[1:])
	require.Nil(t, res)
	require.NotNil(t, err)
}

func TestClientServerError(t *testing.T) {
	an := newAutoNAT(t, nil, allowAll)
	addrs := an.host.Addrs()

	p := bhost.NewBlankHost(swarmt.GenSwarm(t))
	an.host.Peerstore().AddAddrs(p.ID(), p.Addrs(), peerstore.PermanentAddrTTL)
	an.host.Peerstore().AddProtocols(p.ID(), DialProtocol)
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
	an.host.Peerstore().AddAddrs(p.ID(), p.Addrs(), peerstore.PermanentAddrTTL)
	an.host.Peerstore().AddProtocols(p.ID(), DialProtocol)
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
	an.host.Peerstore().AddAddrs(p.ID(), p.Addrs(), peerstore.PermanentAddrTTL)
	an.host.Peerstore().AddProtocols(p.ID(), DialProtocol)
	an.cli.Register()

	tests := []struct {
		handler func(network.Stream)
		success bool
	}{
		{
			handler: func(s network.Stream) {
				r := pbio.NewDelimitedReader(s, maxMsgSize)
				var msg pbv2.Message
				r.ReadMsg(&msg)
				resp := &pbv2.DialResponse{
					Status:       pbv2.DialResponse_ResponseStatus_OK,
					DialStatuses: []pbv2.DialStatus{pbv2.DialStatus_OK},
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
				resp := &pbv2.DialResponse{
					Status: pbv2.DialResponse_ResponseStatus_OK,
					DialStatuses: []pbv2.DialStatus{
						pbv2.DialStatus_OK},
				}
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
				resp := &pbv2.DialResponse{
					Status: pbv2.DialResponse_ResponseStatus_OK,
					DialStatuses: []pbv2.DialStatus{
						pbv2.DialStatus_E_TRANSPORT_NOT_SUPPORTED,
						pbv2.DialStatus_OK,
					},
				}
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
				resp := &pbv2.DialResponse{
					Status: pbv2.DialResponse_ResponseStatus_OK,
					DialStatuses: []pbv2.DialStatus{
						pbv2.DialStatus_E_TRANSPORT_NOT_SUPPORTED,
						pbv2.DialStatus_OK,
					},
				}
				w.WriteMsg(&pbv2.Message{
					Msg: &pbv2.Message_DialResponse{
						DialResponse: resp,
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
				for i := 0; i < len(res); i++ {
					require.NotEqual(t, res[i].Status, pbv2.DialStatus_OK)
					require.Equal(t, res[i].Reachability, network.ReachabilityUnknown)
				}
			} else {
				success := false
				for i := 0; i < len(res); i++ {
					if res[i].Reachability == network.ReachabilityPublic {
						success = true
						break
					}
				}
				if !success {
					t.Fatal("expected one address to be reachable")
				}
			}
		})
	}
}
