package basichost

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"sync"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/net/gostream"
	libp2ptls "github.com/libp2p/go-libp2p/p2p/security/tls"
	"github.com/multiformats/go-multiaddr"
	ma "github.com/multiformats/go-multiaddr"
	multiaddrnet "github.com/multiformats/go-multiaddr/net"
)

type HTTPConfig struct {
	// Whether to enable http request response style protocols
	EnableHTTP bool
	// The multiaddr to use to serve a standard TCP+TLS HTTP server, may be nil
	HTTPServerAddr ma.Multiaddr
}

type addrState interface {
	Addrs(p peer.ID) []ma.Multiaddr
}

type peerConnState interface {
	Connectedness(peer.ID) network.Connectedness
}

type httpHost struct {
	privKey crypto.PrivKey
	ps      addrState
	c       peerConnState
	s       gostream.Streamer

	listenAddrsMu sync.Mutex
	listenAddrs   []ma.Multiaddr

	handler       http.ServeMux
	streamHandler http.ServeMux
	clientsMu     sync.Mutex
	clients       map[peer.ID]*http.Client
	server        http.Server
	streamServer  http.Server
	certToPeer    memoizedCertToPeer
}

func (h *httpHost) NewHTTPClient(p peer.ID) (*http.Client, error) {
	h.clientsMu.Lock()
	defer h.clientsMu.Unlock()

	// If we don't have any connections, prefer creating an http connection over a libp2p stream connection.

	// TODO If we know this is a basic http connection, should we reuse the client across peers?
	c, ok := h.clients[p]
	if !ok {
		if h.c.Connectedness(p) == network.Connected {
			// We are connected over some existing libp2p transport, let's do http over streams
			var err error
			c, err = newHTTPClientViaStreams(h.s, p)
			if err != nil {
				return nil, err
			}
		} else {
			availAddrs := h.ps.Addrs(p)
			var addrToUse multiaddr.Multiaddr
			var foundAddr bool
			for _, a := range availAddrs {
				multiaddr.ForEach(a, func(c multiaddr.Component) bool {
					proto := c.Protocol().Code
					if proto == multiaddr.P_HTTP || proto == multiaddr.P_HTTPS {
						foundAddr = true
						return false
					}
					return true
				})
				if foundAddr {
					addrToUse = a
					break
				}
			}

			var err error
			if !foundAddr {
				// Fallback to libp2p
				c, err = newHTTPClientViaStreams(h.s, p)
				if err != nil {
					return nil, err
				}
			} else {
				id, err := libp2ptls.NewIdentity(h.privKey)
				if err != nil {
					return nil, err
				}
				tlsConf := id.ConfigWithVerify()
				tlsConf.NextProtos = []string{"h2"}

				httpT := http.Transport{
					TLSClientConfig:   tlsConf,
					ForceAttemptHTTP2: true,
				}

				addrToUse = addrToUse.Decapsulate(multiaddr.StringCast("/tls"))
				addr, err := multiaddrnet.ToNetAddr(addrToUse)
				if err != nil {
					return nil, err
				}

				u, err := url.Parse("https://" + addr.String())
				if err != nil {
					return nil, err
				}
				c = &http.Client{
					Transport: roundTripperWithBaseURL{&httpT, u},
				}
			}
		}

		h.clients[p] = c
	}
	return c, nil
}

func (h *httpHost) SetHTTPHandler(pattern string, handler func(peer.ID, http.ResponseWriter, *http.Request)) {
	h.handler.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		peer, err := h.certToPeer.get(r.TLS.PeerCertificates[0].Raw, r.TLS)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		handler(peer, w, r)
	})

	h.streamHandler.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		peer, err := peer.Decode(r.RemoteAddr)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		handler(peer, w, r)
	})
}

func (h *httpHost) ListenHTTPOverLibp2p() error {
	// Listen on streams as well
	streamListener, err := gostream.Listen(h.s, gostream.DefaultP2PProtocol)
	if err != nil {
		return err
	}
	defer streamListener.Close()
	h.streamServer.Handler = &h.streamHandler

	return h.streamServer.Serve(streamListener)
}

func (h *httpHost) ListenHTTP(ma multiaddr.Multiaddr) error {
	id, err := libp2ptls.NewIdentity(h.privKey)
	if err != nil {
		return err
	}
	tlsConf := id.ConfigWithVerify()
	tlsConf.NextProtos = []string{"h2", "http/1.1"}

	addr, err := multiaddrnet.ToNetAddr(ma.Decapsulate(multiaddr.StringCast("/tls")))
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr.String())
	if err != nil {
		return err
	}
	defer ln.Close()

	// Set listen addr
	listenMa, err := multiaddrnet.FromNetAddr(ln.Addr())
	if err != nil {
		return err
	}
	listenMa = listenMa.Encapsulate(multiaddr.StringCast("/tls/http"))
	h.listenAddrsMu.Lock()
	h.listenAddrs = append(h.listenAddrs, listenMa)
	h.listenAddrsMu.Unlock()

	h.server.TLSConfig = tlsConf
	h.server.Handler = &h.handler

	return h.server.ServeTLS(ln, "", "")
}

func (h *httpHost) ListenAddrs() []multiaddr.Multiaddr {
	h.listenAddrsMu.Lock()
	defer h.listenAddrsMu.Unlock()
	out := make([]multiaddr.Multiaddr, 0, len(h.listenAddrs))
	out = append(out, h.listenAddrs...)
	return out
}

// Cached cert to peer id

type memoizedCertToPeer struct {
	mu sync.RWMutex
	m  map[uint64]peer.ID
}

func (m *memoizedCertToPeer) get(cert []byte, owningObj any) (peer.ID, error) {
	h := sha256.New()
	_, err := h.Write(cert)
	if err != nil {
		return "", err
	}

	// Avoid allocating
	b := [32]byte{}
	h.Sum(b[:0])

	k := binary.LittleEndian.Uint64(b[0:8])

	m.mu.RLock()
	val, ok := m.m[k]
	m.mu.RUnlock()
	if !ok {

		cert, err := x509.ParseCertificate(cert)
		if err != nil {
			return "", err
		}
		certs := []*x509.Certificate{cert}
		pubkey, err := libp2ptls.PubKeyFromCertChain(certs)
		if err != nil {
			return "", err
		}

		val, err = peer.IDFromPublicKey(pubkey)
		if err != nil {
			return "", err
		}
		m.mu.Lock()
		m.m[k] = val
		m.mu.Unlock()

		runtime.SetFinalizer(owningObj, func(owningObj any) {
			m.mu.Lock()
			defer m.mu.Unlock()
			delete(m.m, k)
		})

	}

	return val, nil
}
