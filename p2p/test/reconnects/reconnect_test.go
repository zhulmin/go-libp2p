package reconnect

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"runtime/pprof"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p-core/sec/insecure"

	noise "github.com/libp2p/go-libp2p-noise"

	ma "github.com/multiformats/go-multiaddr"

	csms "github.com/libp2p/go-conn-security-multistream"
	"github.com/libp2p/go-libp2p-core/crypto"
	yamux "github.com/libp2p/go-libp2p-yamux"
	msmux "github.com/libp2p/go-stream-muxer-multistream"

	tptu "github.com/libp2p/go-libp2p-transport-upgrader"
	"github.com/libp2p/go-tcp-transport"

	"github.com/libp2p/go-libp2p-core/peer"

	bhost "github.com/libp2p/go-libp2p/p2p/host/basic"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/protocol"

	swarmt "github.com/libp2p/go-libp2p-swarm/testing"

	u "github.com/ipfs/go-ipfs-util"
	logging "github.com/ipfs/go-log/v2"
	"github.com/stretchr/testify/require"
)

var log = logging.Logger("reconnect")

func EchoStreamHandler(stream network.Stream) {
	// c := stream.Conn()
	// log.Debugf("%s echoing %s", c.LocalPeer(), c.RemotePeer())
	go func() {
		_, err := io.Copy(stream, stream)
		if err == nil {
			stream.Close()
		} else {
			stream.Reset()
		}
	}()
}

type sendChans struct {
	send   chan struct{}
	sent   chan struct{}
	read   chan struct{}
	close_ chan struct{}
	closed chan struct{}
}

func newSendChans() sendChans {
	return sendChans{
		send:   make(chan struct{}),
		sent:   make(chan struct{}),
		read:   make(chan struct{}),
		close_: make(chan struct{}),
		closed: make(chan struct{}),
	}
}

func newSender() (chan sendChans, func(s network.Stream)) {
	scc := make(chan sendChans)
	return scc, func(s network.Stream) {
		sc := newSendChans()
		scc <- sc

		defer func() {
			s.Close()
			sc.closed <- struct{}{}
		}()

		buf := make([]byte, 65536)
		buf2 := make([]byte, 65536)
		u.NewTimeSeededRand().Read(buf)

		for {
			select {
			case <-sc.close_:
				return
			case <-sc.send:
			}

			// send a randomly sized subchunk
			from := rand.Intn(len(buf) / 2)
			to := rand.Intn(len(buf) / 2)
			sendbuf := buf[from : from+to]

			// log.Debugf("sender sending %d bytes", len(sendbuf))
			_, err := s.Write(sendbuf)
			if err != nil {
				// log.Debug("sender error. exiting:", err)
				return
			}

			// log.Debugf("sender wrote %d bytes", n)
			sc.sent <- struct{}{}

			if _, err := io.ReadFull(s, buf2[:len(sendbuf)]); err != nil {
				// log.Debug("sender error. failed to read:", err)
				return
			}

			// log.Debugf("sender read %d bytes", n)
			sc.read <- struct{}{}
		}
	}
}

func TestConnectHangSwarm(t *testing.T) {
	h1 := swarmt.GenSwarm(t, swarmt.OptDisableQUIC)
	h2 := swarmt.GenSwarm(t, swarmt.OptDisableQUIC)

	h1.Peerstore().AddAddrs(h2.LocalPeer(), h2.ListenAddresses(), time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// conn, err := h1.DialPeer(ctx, h2.LocalPeer())
	// require.NoError(t, err)

	done := make(chan struct{})
	streamAccepted := make(chan struct{})
	go func() {
		h2.SetStreamHandler(func(str network.Stream) {
			defer close(done)
			b := make([]byte, 6)
			_, err := str.Read(b)
			require.NoError(t, err)
			close(streamAccepted)
		})
	}()
	str, err := h1.NewStream(ctx, h2.LocalPeer())
	require.NoError(t, err)
	_, err = str.Write([]byte("foobar"))
	require.NoError(t, err)
	<-streamAccepted
	require.NoError(t, str.CloseWrite())

	fmt.Println(h1.Conns()[0])
	require.NoError(t, h1.Conns()[0].Close())
	require.Eventually(t, func() bool { return len(h1.Conns()) == 0 }, 10*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool { return len(h2.Conns()) == 0 }, 10*time.Second, 10*time.Millisecond)
}

func TestConnectHangHost(t *testing.T) {
	h1, err := bhost.NewHost(swarmt.GenSwarm(t, swarmt.OptDisableQUIC), nil)
	require.NoError(t, err)
	h2, err := bhost.NewHost(swarmt.GenSwarm(t, swarmt.OptDisableQUIC), nil)
	require.NoError(t, err)
	defer h1.Close()
	defer h2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, h1.Connect(ctx, peer.AddrInfo{ID: h2.ID(), Addrs: h2.Addrs()}))
	require.Len(t, h1.Network().Conns(), 1)
	require.Len(t, h2.Network().Conns(), 1)

	done := make(chan struct{})
	streamAccepted := make(chan struct{})
	h2.SetStreamHandler(protocol.TestingID, func(str network.Stream) {
		defer close(done)
		b := make([]byte, 6)
		_, err := str.Read(b)
		require.NoError(t, err)
		close(streamAccepted)
		require.Equal(t, b, []byte("foobar"))
	})
	str, err := h1.NewStream(ctx, h2.ID(), protocol.TestingID)
	require.NoError(t, err)
	_, err = str.Write([]byte("foobar"))
	require.NoError(t, err)
	<-streamAccepted
	require.NoError(t, str.CloseWrite())

	fmt.Println(h1.Network().Conns()[0])
	require.NoError(t, h1.Network().Conns()[0].Close())

	waitForClose(t, []host.Host{h1, h2})
	// require.Eventually(t, func() bool { return len(h1.Network().Conns()) == 0 }, 10*time.Second, 10*time.Millisecond)
	// require.Eventually(t, func() bool { return len(h2.Network().Conns()) == 0 }, 10*time.Second, 10*time.Millisecond)
}

func makeUpgrader(t *testing.T) (peer.ID, *tptu.Upgrader) {
	t.Helper()
	priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519, 256)
	require.NoError(t, err)
	id, err := peer.IDFromPrivateKey(priv)
	require.NoError(t, err)
	secMuxer := new(csms.SSMuxer)
	tlsTr, err := noise.New(priv)
	require.NoError(t, err)
	// secMuxer.AddTransport(noise.ID, tlsTr)
	_ = tlsTr
	secMuxer.AddTransport(insecure.ID, insecure.NewWithIdentity(id, priv))
	stMuxer := msmux.NewBlankTransport()
	stMuxer.AddTransport("/yamux/1.0.0", yamux.DefaultTransport)
	return id, &tptu.Upgrader{
		Secure: secMuxer,
		Muxer:  stMuxer,
	}
}

func TestConnectHangMinimal(t *testing.T) {
	_, upgrader1 := makeUpgrader(t)
	tr1, err := tcp.NewTCPTransport(upgrader1)
	require.NoError(t, err)
	id2, upgrader2 := makeUpgrader(t)
	tr2, err := tcp.NewTCPTransport(upgrader2)
	require.NoError(t, err)

	ln, err := tr2.Listen(ma.StringCast("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	done := make(chan struct{})
	streamAccepted := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		require.NoError(t, err)
		str, err := conn.AcceptStream()
		require.NoError(t, err)
		close(streamAccepted)
		io.ReadAll(str)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := tr1.Dial(ctx, ln.Multiaddr(), id2)
	require.NoError(t, err)
	str, err := conn.OpenStream(ctx)
	require.NoError(t, err)
	_, err = str.Write([]byte("foobar"))
	require.NoError(t, err)
	<-streamAccepted

	require.NoError(t, conn.Close())

	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, 10*time.Second, 10*time.Millisecond)

	// fmt.Println(h1.Network().Conns()[0])
	// require.NoError(t, h1.Network().Conns()[0].Close())
	// require.Eventually(t, func() bool { return len(h1.Network().Conns()) == 0 }, 10*time.Second, 10*time.Millisecond)
	// require.Eventually(t, func() bool { return len(h2.Network().Conns()) == 0 }, 10*time.Second, 10*time.Millisecond)
}

// TestReconnect tests whether hosts are able to disconnect and reconnect.
func TestReconnect2(t *testing.T) {
	h1, err := bhost.NewHost(swarmt.GenSwarm(t, swarmt.OptDisableQUIC), nil)
	require.NoError(t, err)
	h2, err := bhost.NewHost(swarmt.GenSwarm(t, swarmt.OptDisableQUIC), nil)
	require.NoError(t, err)
	hosts := []host.Host{h1, h2}

	h1.SetStreamHandler(protocol.TestingID, EchoStreamHandler)
	h2.SetStreamHandler(protocol.TestingID, EchoStreamHandler)

	rounds := 1
	// if testing.Short() {
	// 	rounds = 4
	// }
	for i := 0; i < rounds; i++ {
		log.Debugf("TestReconnect: %d/%d\n", i, rounds)
		subtestConnSendDisc(t, hosts)
	}
}

// TestReconnect tests whether hosts are able to disconnect and reconnect.
func TestReconnect5(t *testing.T) {
	const num = 5
	hosts := make([]host.Host, 0, num)
	for i := 0; i < num; i++ {
		h, err := bhost.NewHost(swarmt.GenSwarm(t), nil)
		require.NoError(t, err)
		h.SetStreamHandler(protocol.TestingID, EchoStreamHandler)
		hosts = append(hosts, h)
	}

	rounds := 4
	if testing.Short() {
		rounds = 2
	}
	for i := 0; i < rounds; i++ {
		log.Debugf("TestReconnect: %d/%d\n", i, rounds)
		subtestConnSendDisc(t, hosts)
	}
}

func subtestConnSendDisc(t *testing.T, hosts []host.Host) {
	ctx := context.Background()
	numStreams := 3 * len(hosts)
	numMsgs := 10

	if testing.Short() {
		numStreams = 5 * len(hosts)
		numMsgs = 4
	}

	ss, sF := newSender()

	for _, h1 := range hosts {
		for _, h2 := range hosts {
			if h1.ID() >= h2.ID() {
				continue
			}

			h2pi := h2.Peerstore().PeerInfo(h2.ID())
			log.Debugf("dialing %s", h2pi.Addrs)
			require.NoError(t, h1.Connect(ctx, h2pi), "failed to connect")
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < numStreams; i++ {
		h1 := hosts[i%len(hosts)]
		h2 := hosts[(i+1)%len(hosts)]
		s, err := h1.NewStream(context.Background(), h2.ID(), protocol.TestingID)
		require.NoError(t, err)

		wg.Add(1)
		go func(j int) {
			defer wg.Done()

			go sF(s)
			// log.Debugf("getting handle %d", j)
			sc := <-ss // wait to get handle.
			// log.Debugf("spawning worker %d", j)

			for k := 0; k < numMsgs; k++ {
				sc.send <- struct{}{}
				<-sc.sent
				// log.Debugf("%d sent %d", j, k)
				<-sc.read
				// log.Debugf("%d read %d", j, k)
			}
			sc.close_ <- struct{}{}
			<-sc.closed
			// log.Debugf("closed %d", j)
		}(i)
	}
	wg.Wait()

	for i, h1 := range hosts {
		fmt.Printf("host %d has %d conns: %+v\n", i, len(h1.Network().Conns()), h1.Network().Conns())
		// log.Debugf("host %d has %d conns", i, len(h1.Network().Conns()))
	}

	for _, h1 := range hosts {
		// close connection
		cs := h1.Network().Conns()
		for _, c := range cs {
			if c.LocalPeer() > c.RemotePeer() {
				continue
			}
			log.Debugf("closing: %s", c)
			fmt.Printf("closing: %s\n", c)
			require.NoError(t, c.Close())
		}
	}

	// require.Eventuallyf(t, func() bool {
	// 	for _, h := range hosts {
	// 		if len(h.Network().Conns()) > 0 {
	// 			return false
	// 		}
	// 	}
	// 	return true
	// }, 10*time.Second, 10*time.Millisecond, "failed to close all connections")
	waitForClose(t, hosts)
}

func waitForClose(t *testing.T, hosts []host.Host) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Millisecond)
	waitloop:
		for range ticker.C {
			for _, h := range hosts {
				if len(h.Network().Conns()) > 0 {
					continue waitloop
				}
			}
			return
		}
	}()

	for {
		select {
		case <-done:
			return
		case <-time.After(10 * time.Second):
			fmt.Println("test hanging")
			for i, h1 := range hosts {
				fmt.Printf("host %d has %d conns: %+v\n", i, len(h1.Network().Conns()), h1.Network().Conns())
				// log.Debugf("host %d has %d conns", i, len(h1.Network().Conns()))
			}
			// debug.PrintStack()
			pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
			t.Fatal("fail")
		}
	}
}
