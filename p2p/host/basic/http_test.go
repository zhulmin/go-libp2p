package basichost

import (
	"bytes"
	"context"
	"crypto/x509"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	coretest "github.com/libp2p/go-libp2p/core/test"
	swarmt "github.com/libp2p/go-libp2p/p2p/net/swarm/testing"
	libp2ptls "github.com/libp2p/go-libp2p/p2p/security/tls"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func TestHTTPRoundTripOverHTTP(t *testing.T) {
	ma := multiaddr.StringCast("/ip4/127.0.0.1/tcp/9561/tls/http")
	h1, err := NewHost(swarmt.GenSwarm(t), &HostOpts{
		HTTPConfig: HTTPConfig{
			EnableHTTP:     true,
			HTTPServerAddr: ma,
		},
	})
	require.NoError(t, err)
	defer h1.Close()
	h2, err := NewHost(swarmt.GenSwarm(t), nil)
	require.NoError(t, err)
	defer h2.Close()

	recvdPeer := make(chan peer.ID, 1)
	h1.SetHTTPHandler("/echo", func(peer peer.ID, w http.ResponseWriter, r *http.Request) {
		// Select so we never block this handler. Useful if you want to keep this server up and use it directly with curl
		select {
		case recvdPeer <- peer:
		default:
		}
		w.WriteHeader(200)
		io.Copy(w, r.Body)
	})

	// We could use h1.Addrs, but we want to test that we are indeed listening over TCP+TLS HTTP
	h2.Peerstore().AddAddrs(h1.ID(), h1.httpHost.ListenAddrs(), time.Hour)
	client, err := h2.NewHTTPClient(h1.ID())
	require.NoError(t, err)

	resp, err := client.Post("/echo", "application/octet-stream", bytes.NewReader([]byte("Hello World")))
	require.NoError(t, err)
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Equal(t, string(respBytes), "Hello World")
	require.Equal(t, h2.ID(), <-recvdPeer)
}

func TestHTTPRoundTripOverLibp2pTransport(t *testing.T) {
	ma := multiaddr.StringCast("/ip4/127.0.0.1/tcp/9561/tls/http")
	h1, err := NewHost(swarmt.GenSwarm(t), &HostOpts{
		HTTPConfig: HTTPConfig{
			EnableHTTP:     true,
			HTTPServerAddr: ma,
		},
	})
	require.NoError(t, err)
	defer h1.Close()
	h2, err := NewHost(swarmt.GenSwarm(t), nil)
	require.NoError(t, err)
	defer h2.Close()

	recvdPeer := make(chan peer.ID, 1)
	h1.SetHTTPHandler("/echo", func(peer peer.ID, w http.ResponseWriter, r *http.Request) {
		// Select so we never block this handler. Useful if you want to keep this server up and use it directly with curl
		select {
		case recvdPeer <- peer:
		default:
		}
		w.WriteHeader(200)
		io.Copy(w, r.Body)
	})

	ctx := context.Background()
	h2.Connect(ctx, peer.AddrInfo{
		ID: h1.ID(),
		// TODO filter out http addrs here
		Addrs: h1.Addrs(),
	})

	client, err := h2.NewHTTPClient(h1.ID())
	require.NoError(t, err)

	resp, err := client.Post("/echo", "application/octet-stream", bytes.NewReader([]byte("Hello World")))
	require.NoError(t, err)
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Equal(t, string(respBytes), "Hello World")
	require.Equal(t, h2.ID(), <-recvdPeer)
}

func BenchmarkReadingPeerFromCert(b *testing.B) {
	priv, _, err := coretest.RandTestKeyPair(crypto.Ed25519, 256)
	require.NoError(b, err)
	id, err := libp2ptls.NewIdentity(priv)
	require.NoError(b, err)

	tlsConf, _ := id.ConfigForPeer("")
	cert, err := x509.ParseCertificate(tlsConf.Certificates[0].Certificate[0])
	require.NoError(b, err)
	certs := []*x509.Certificate{cert}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// This is the easiest way we would get the peer id from an http request, we read it from the tls cert every time.
		pubkey, err := libp2ptls.PubKeyFromCertChain(certs)
		require.NoError(b, err)
		_, err = peer.IDFromPublicKey(pubkey)
		require.NoError(b, err)
	}
}

func BenchmarkReadingPeerFromCertMemoized(b *testing.B) {
	priv, _, err := coretest.RandTestKeyPair(crypto.Ed25519, 256)
	require.NoError(b, err)
	id, err := libp2ptls.NewIdentity(priv)
	require.NoError(b, err)

	tlsConf, _ := id.ConfigForPeer("")
	cert := tlsConf.Certificates[0].Certificate[0]
	require.NoError(b, err)
	m := memoizedCertToPeer{
		m: map[uint64]peer.ID{},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// This is the fastest way we would get the peer id from an http
		// request, we memoize the peer id from the hash of the cert. Then we
		// add a finalizer to some owning object (here it's a tlsConf, but
		// normally it would be a pointer to the tls cert) so that the memoized
		// map gets cleared.
		_, err := m.get(cert, tlsConf)
		require.NoError(b, err)
	}
}
