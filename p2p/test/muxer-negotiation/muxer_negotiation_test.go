package muxernegotiation

import (
	"bufio"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"testing"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/muxer/mplex"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/stretchr/testify/require"

	/*
		"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	*/

	golog "github.com/ipfs/go-log/v2"
	ma "github.com/multiformats/go-multiaddr"
)

const testData = "Muxer Test Data\n"

type muxerTest struct {
	svrMuxers     []string
	svrTrans      []network.Multiplexer
	cliMuxers     []string
	cliTrans      []network.Multiplexer
	expectedMuxer string
}

func TestMuxerNegotiatin(t *testing.T) {
	secTypes := []string{Tls, Noise}
	testCases := []muxerTest{
		{svrMuxers: []string{"/mplex/6.7.0"},
			svrTrans:      []network.Multiplexer{mplex.DefaultTransport},
			cliMuxers:     []string{"/yamux/1.0.0"},
			cliTrans:      []network.Multiplexer{yamux.DefaultTransport},
			expectedMuxer: ""},
		{svrMuxers: []string{"/mplex/6.7.0"},
			svrTrans:      []network.Multiplexer{mplex.DefaultTransport},
			cliMuxers:     []string{"/mplex/6.7.0"},
			cliTrans:      []network.Multiplexer{mplex.DefaultTransport},
			expectedMuxer: "/mplex/6.7.0"},
		{svrMuxers: []string{"/yamux/1.0.0"},
			svrTrans:      []network.Multiplexer{yamux.DefaultTransport},
			cliMuxers:     []string{"/yamux/1.0.0"},
			cliTrans:      []network.Multiplexer{yamux.DefaultTransport},
			expectedMuxer: "/yamux/1.0.0"},
		{svrMuxers: []string{"/yamux/1.0.0", "/mplex/6.7.0"},
			svrTrans:      []network.Multiplexer{yamux.DefaultTransport, mplex.DefaultTransport},
			cliMuxers:     []string{"/mplex/6.7.0", "/yamux/1.0.0"},
			cliTrans:      []network.Multiplexer{mplex.DefaultTransport, yamux.DefaultTransport},
			expectedMuxer: "/yamux/1.0.0"},
		{svrMuxers: []string{"/mplex/6.7.0", "/yamux/1.0.0"},
			svrTrans:      []network.Multiplexer{mplex.DefaultTransport, yamux.DefaultTransport},
			cliMuxers:     []string{"/yamux/1.0.0", "/mplex/6.7.0"},
			cliTrans:      []network.Multiplexer{yamux.DefaultTransport, mplex.DefaultTransport},
			expectedMuxer: "/mplex/6.7.0"},
	}

	doMuxerNegotiation := func(t *testing.T, secType string, svrMuxers, cliMuxers []string, svrTrans, cliTrans []network.Multiplexer, expected string) {
		sctx, sCancel := context.WithCancel(context.Background())
		cctx, cCancel := context.WithCancel(context.Background())

		sSec, svrh, err := makeHost(secType, svrMuxers, svrTrans, 58568)
		require.NoError(t, err)
		require.NotNil(t, sSec)
		require.NotNil(t, svrh)

		cSec, clih, err := makeHost(secType, cliMuxers, cliTrans, 59569)
		require.NoError(t, err)
		require.NotNil(t, cSec)
		require.NotNil(t, clih)

		ready := make(chan struct{})
		go func() {
			runServer(sctx, svrh)
			close(ready)
			<-sctx.Done()
		}()

		<-ready
		runClientAndCheckMuxer(cctx, clih, getHostAddress(svrh), cSec, sSec, expected, t)
		clih.Close()
		svrh.Close()
		cCancel()
		sCancel()
	}

	golog.SetAllLoggers(golog.LevelInfo)

	for _, secType := range secTypes {
		for i, testCase := range testCases {
			testName := "Test muxer negotiation for " + secType + ", case " + fmt.Sprint(i)
			t.Run(testName, func(t *testing.T) {
				doMuxerNegotiation(t, secType, testCase.svrMuxers, testCase.cliMuxers, testCase.svrTrans, testCase.cliTrans, testCase.expectedMuxer)
			})
		}
	}
}

func makeHost(transportType string, muxers []string, muxTrans []network.Multiplexer, port int) (*TransportWithMuxer, host.Host, error) {
	r := rand.Reader

	priv, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, r)
	if err != nil {
		return nil, nil, err
	}

	secTrans, err := New(priv, muxers, transportType)
	if err != nil {
		return nil, nil, err
	}

	opts := []libp2p.Option{
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port)),
		//libp2p.DefaultListenAddrs,
		libp2p.Identity(priv),
		libp2p.Security(transportType, secTrans),
	}
	for i := 0; i < len(muxers); i++ {
		opts = append(opts, libp2p.Muxer(muxers[i], muxTrans[i]))
	}
	h, err := libp2p.New(opts...)
	return secTrans, h, err
}

func getHostAddress(ha host.Host) string {
	// Build host multiaddress
	hostAddr, _ := ma.NewMultiaddr(fmt.Sprintf("/p2p/%s", ha.ID().Pretty()))

	addr := ha.Addrs()[0]
	return addr.Encapsulate(hostAddr).String()
}

func runServer(ctx context.Context, ha host.Host) {
	ha.SetStreamHandler("/echo/1.0.0", streamHandler)
}

func runClientAndCheckMuxer(ctx context.Context, ha host.Host, targetPeer string, cSec, sSec *TransportWithMuxer, expected string, t *testing.T) {
	ha.SetStreamHandler("/echo/1.0.0", streamHandler)
	maddr, err := ma.NewMultiaddr(targetPeer)
	require.NoError(t, err)

	info, err := peer.AddrInfoFromP2pAddr(maddr)
	require.NoError(t, err)

	log.Println("Connecting to server at ", info.ID)
	ha.Peerstore().AddAddrs(info.ID, info.Addrs, peerstore.PermanentAddrTTL)

	s, err := ha.NewStream(context.Background(), info.ID, "/echo/1.0.0")
	if expected == "" {
		require.Error(t, err)
	} else {
		require.NoError(t, err)
	}

	// Check the muxers selected on both sides.
	require.Equal(t, expected, cSec.selectedMuxer)
	require.Equal(t, expected, sSec.selectedMuxer)
	if expected == "" {
		return
	}

	// Verify data stream muxer works.
	_, err = s.Write([]byte(testData))
	require.NoError(t, err)

	out, err := io.ReadAll(s)
	require.NoError(t, err)
	require.Equal(t, []byte(testData), out)
}

func echoData(s network.Stream) error {
	buf := bufio.NewReader(s)
	str, err := buf.ReadString('\n')
	if err != nil {
		return err
	}

	_, err = s.Write([]byte(str))
	return err
}

func streamHandler(s network.Stream) {
	if err := echoData(s); err != nil {
		log.Println(err)
	}
	s.Close()
}
