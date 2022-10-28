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

	ma "github.com/multiformats/go-multiaddr"
)

const (
	testData   = "Muxer Test Data\n"
	serverPort = 58568
	clientPort = 58569
)

type muxerTest struct {
	svrMuxers     []MuxerEntity
	cliMuxers     []MuxerEntity
	expectedMuxer string
}

func TestMuxerNegotiatin(t *testing.T) {
	secTypes := []string{Tls, Noise}
	testCases := []muxerTest{
		{svrMuxers: []MuxerEntity{{"/mplex/6.7.0", mplex.DefaultTransport}},
			cliMuxers:     []MuxerEntity{{"/yamux/1.0.0", yamux.DefaultTransport}},
			expectedMuxer: ""},
		{svrMuxers: []MuxerEntity{{"/mplex/6.7.0", mplex.DefaultTransport}},
			cliMuxers:     []MuxerEntity{{"/mplex/6.7.0", mplex.DefaultTransport}},
			expectedMuxer: "/mplex/6.7.0"},
		{svrMuxers: []MuxerEntity{{"/yamux/1.0.0", yamux.DefaultTransport}},
			cliMuxers:     []MuxerEntity{{"/yamux/1.0.0", yamux.DefaultTransport}},
			expectedMuxer: "/yamux/1.0.0"},
		{svrMuxers: []MuxerEntity{{"/yamux/1.0.0", yamux.DefaultTransport},
			{"/mplex/6.7.0", mplex.DefaultTransport}},
			cliMuxers: []MuxerEntity{{"/mplex/6.7.0", mplex.DefaultTransport},
				{"/yamux/1.0.0", yamux.DefaultTransport}},
			expectedMuxer: "/yamux/1.0.0"},
		{svrMuxers: []MuxerEntity{{"/mplex/6.7.0", mplex.DefaultTransport},
			{"/yamux/1.0.0", yamux.DefaultTransport}},
			cliMuxers: []MuxerEntity{{"/yamux/1.0.0", yamux.DefaultTransport},
				{"/mplex/6.7.0", mplex.DefaultTransport}},
			expectedMuxer: "/mplex/6.7.0"},
	}

	doMuxerNegotiation := func(t *testing.T, secType string, svrMuxers, cliMuxers []MuxerEntity, expected string) {
		sctx, sCancel := context.WithCancel(context.Background())
		cctx, cCancel := context.WithCancel(context.Background())

		sSec, svrh := makeHost(t, secType, svrMuxers, serverPort)
		cSec, clih := makeHost(t, secType, cliMuxers, clientPort)

		ready := make(chan struct{})
		// Run server.
		go func() {
			svrh.SetStreamHandler("/echo/1.0.0", streamHandler)
			close(ready)
			<-sctx.Done()
		}()

		<-ready
		runClientAndCheckMuxer(t, cctx, clih, getHostAddress(t, svrh), cSec, sSec, expected)
		clih.Close()
		svrh.Close()
		cCancel()
		sCancel()
	}

	for _, secType := range secTypes {
		for i, testCase := range testCases {
			testName := "Test muxer negotiation for " + secType + " case " + fmt.Sprint(i)
			t.Run(testName, func(t *testing.T) {
				doMuxerNegotiation(t, secType, testCase.svrMuxers, testCase.cliMuxers, testCase.expectedMuxer)
			})
		}
	}
}

func makeHost(t *testing.T, transportType string, muxers []MuxerEntity, port int) (*TransportWithMuxer, host.Host) {
	r := rand.Reader

	priv, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, r)
	require.NoError(t, err)

	secTrans, err := New(priv, muxers, transportType)
	require.NoError(t, err)

	opts := []libp2p.Option{
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port)),
		libp2p.Identity(priv),
		libp2p.Security(transportType, secTrans),
	}
	for _, muxer := range muxers {
		opts = append(opts, libp2p.Muxer(muxer.id, muxer.trans))
	}
	h, err := libp2p.New(opts...)
	require.NoError(t, err)

	return secTrans, h
}

func getHostAddress(t *testing.T, ha host.Host) *peer.AddrInfo {
	// Build host multiaddress
	hostAddr := ma.StringCast(fmt.Sprintf("/p2p/%s", ha.ID().Pretty()))
	addr := ha.Addrs()[0]
	addrInfo, err := peer.AddrInfoFromString(addr.Encapsulate(hostAddr).String())
	require.NoError(t, err)
	return addrInfo
}

func runClientAndCheckMuxer(t *testing.T, ctx context.Context, ha host.Host, info *peer.AddrInfo, cSec, sSec *TransportWithMuxer, expected string) {
	ha.SetStreamHandler("/echo/1.0.0", streamHandler)

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
