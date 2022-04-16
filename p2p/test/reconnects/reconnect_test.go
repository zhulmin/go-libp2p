package reconnect

import (
	"context"
	"io"
	"math/rand"
	"sync"
	"testing"
	"time"

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
	c := stream.Conn()
	log.Debugf("%s echoing %s", c.LocalPeer(), c.RemotePeer())
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
	send   chan struct{} // signals that a message should be sent
	sent   chan struct{} // receives when a message has been sent
	read   chan struct{} // receives when a message has ben read
	close  chan struct{} // used to signal that the host should be closed
	closed chan struct{} // is closed when the host is closed
}

func newSendChans() sendChans {
	return sendChans{
		send:   make(chan struct{}),
		sent:   make(chan struct{}),
		read:   make(chan struct{}),
		close:  make(chan struct{}),
		closed: make(chan struct{}),
	}
}

func newSender() (chan sendChans, func(s network.Stream)) {
	scc := make(chan sendChans)
	return scc, func(s network.Stream) {
		sc := newSendChans()
		scc <- sc

		defer func() {
			defer close(sc.closed)
			s.Close()
		}()

		buf := make([]byte, 65536)
		buf2 := make([]byte, 65536)
		u.NewTimeSeededRand().Read(buf)

		for {
			select {
			case <-sc.close:
				return
			case <-sc.send:
			}

			// send a randomly sized subchunk
			from := rand.Intn(len(buf) / 2)
			to := rand.Intn(len(buf)/2) + 1
			sendbuf := buf[from : from+to]

			log.Debugf("sender sending %d bytes", len(sendbuf))
			n, err := s.Write(sendbuf)
			if err != nil {
				log.Debug("sender error. exiting:", err)
				return
			}

			log.Debugf("sender wrote %d bytes", n)
			sc.sent <- struct{}{}

			if n, err = io.ReadFull(s, buf2[:len(sendbuf)]); err != nil {
				log.Debug("sender error. failed to read:", err)
				return
			}

			log.Debugf("sender read %d bytes", n)
			sc.read <- struct{}{}
		}
	}
}

// TestReconnect tests whether hosts are able to disconnect and reconnect.
func TestReconnect2(t *testing.T) {
	// TCP RST handling is flaky in OSX, see https://github.com/golang/go/issues/50254.
	// We can avoid this by using QUIC in this test.
	h1, err := bhost.NewHost(swarmt.GenSwarm(t, swarmt.OptDisableTCP), nil)
	require.NoError(t, err)
	defer h1.Close()
	h2, err := bhost.NewHost(swarmt.GenSwarm(t, swarmt.OptDisableTCP), nil)
	require.NoError(t, err)
	defer h2.Close()
	hosts := []host.Host{h1, h2}

	h1.SetStreamHandler(protocol.TestingID, EchoStreamHandler)
	h2.SetStreamHandler(protocol.TestingID, EchoStreamHandler)

	const rounds = 8
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
		// TCP RST handling is flaky in OSX, see https://github.com/golang/go/issues/50254.
		// We can avoid this by using QUIC in this test.
		h, err := bhost.NewHost(swarmt.GenSwarm(t, swarmt.OptDisableTCP), nil)
		require.NoError(t, err)
		defer h.Close()
		h.SetStreamHandler(protocol.TestingID, EchoStreamHandler)
		hosts = append(hosts, h)
	}

	const rounds = 4
	for i := 0; i < rounds; i++ {
		log.Debugf("TestReconnect: %d/%d\n", i, rounds)
		subtestConnSendDisc(t, hosts)
	}
}

func subtestConnSendDisc(t *testing.T, hosts []host.Host) {
	const numMsgs = 10
	numStreams := 3 * len(hosts)

	ss, sF := newSender()

	// Connect hosts to each other.
	for _, h1 := range hosts {
		for _, h2 := range hosts {
			if h1.ID() >= h2.ID() {
				continue
			}

			h2pi := h2.Peerstore().PeerInfo(h2.ID())
			log.Debugf("dialing %s", h2pi.Addrs)
			require.NoError(t, h1.Connect(context.Background(), h2pi), "failed to connect")
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
			log.Debugf("getting handle %d", j)
			sc := <-ss // wait to get handle.
			log.Debugf("spawning worker %d", j)

			for k := 0; k < numMsgs; k++ {
				sc.send <- struct{}{}
				<-sc.sent
				log.Debugf("%d sent %d", j, k)
				<-sc.read
				log.Debugf("%d read %d", j, k)
			}
			sc.close <- struct{}{}
			<-sc.closed
			log.Debugf("closed %d", j)
		}(i)
	}
	wg.Wait()

	for i, h1 := range hosts {
		log.Debugf("host %d has %d conns", i, len(h1.Network().Conns()))
	}

	for _, h1 := range hosts {
		// close connection
		cs := h1.Network().Conns()
		for _, c := range cs {
			if c.LocalPeer() > c.RemotePeer() {
				continue
			}
			log.Debugf("closing: %s", c)
			c.Close()
		}
	}

	<-time.After(20 * time.Millisecond)

	for i, h := range hosts {
		if len(h.Network().Conns()) > 0 {
			t.Fatalf("host %d %s has %d conns! not zero.", i, h.ID(), len(h.Network().Conns()))
		}
	}
}
