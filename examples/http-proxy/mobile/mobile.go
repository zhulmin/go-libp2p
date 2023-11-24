package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	// We need to import libp2p's libraries that we use in this project.

	ds "github.com/ipfs/go-datastore"
	dsync "github.com/ipfs/go-datastore/sync"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	rhost "github.com/libp2p/go-libp2p/p2p/host/routed"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// Protocol defines the libp2p protocol that we will use for the libp2p proxy
// service that we are going to provide. This will tag the streams used for
// this service. Streams are multiplexed and their protocol tag helps
// libp2p handle them to the right handler functions.
const Protocol = "/yole.app/0.0.1"

// ProxyService provides HTTP proxying on top of libp2p by launching an
// HTTP server which tunnels the requests to a destination peer running
// ProxyService too.
type ProxyService struct {
	host      host.Host
	dest      peer.ID
	proxyAddr ma.Multiaddr
}

// NewProxyService attaches a proxy service to the given libp2p Host.
// The proxyAddr parameter specifies the address on which the
// HTTP proxy server listens. The dest parameter specifies the peer
// ID of the remote peer in charge of performing the HTTP requests.
//
// ProxyAddr/dest may be nil/"" it is not necessary that this host
// provides a listening HTTP server (and instead its only function is to
// perform the proxied http requests it receives from a different peer.
//
// The addresses for the dest peer should be part of the host's peerstore.
//
//export NewProxyService
func NewProxyService(h host.Host, proxyAddr ma.Multiaddr, dest peer.ID) *ProxyService {
	// We let our host know that it needs to handle streams tagged with the
	// protocol id that we have defined, and then handle them to
	// our own streamHandling function.
	h.SetStreamHandler(Protocol, streamHandler)

	fmt.Println("Proxy server is ready")
	fmt.Println("libp2p-peer addresses:")
	for _, a := range h.Addrs() {
		fmt.Printf("%s/ipfs/%s\n", a, h.ID())
	}

	return &ProxyService{
		host:      h,
		dest:      dest,
		proxyAddr: proxyAddr,
	}
}

func streamHandler(stream network.Stream) {
	// Remember to close the stream when we are done.
	defer stream.Close()

	// Create a new buffered reader, as ReadRequest needs one.
	// The buffered reader reads from our stream, on which we
	// have sent the HTTP request (see ServeHTTP())
	buf := bufio.NewReader(stream)
	// Read the HTTP request from the buffer
	req, err := http.ReadRequest(buf)
	if err != nil {
		stream.Reset()
		log.Println(err)
		return
	}
	defer req.Body.Close()

	// We need to reset these fields in the request
	// URL as they are not maintained.
	req.URL.Scheme = "http"
	hp := strings.Split(req.Host, ":")
	if len(hp) > 1 && hp[1] == "443" {
		req.URL.Scheme = "https"
	} else {
		req.URL.Scheme = "http"
	}
	req.URL.Host = req.Host

	outreq := new(http.Request)
	*outreq = *req

	// We now make the request
	fmt.Printf("Making request to %s\n", req.URL)

	//TODO: ...
	resp, err := http.DefaultTransport.RoundTrip(outreq)
	if err != nil {
		stream.Reset()
		log.Println(err)
		return
	}

	// resp.Write writes whatever response we obtained for our
	// request back to the stream.
	// z: 13.把resp写入stream? 是的, 方法说明是
	// Write writes r to w in the HTTP/1.x server response format,

	//   Write writes 'resp to 'writer/stream in the HTTP/1.x server response format
	resp.Write(stream)
}

// Serve listens on the ProxyService's proxy address. This effectively
// allows to set the listening address as http proxy.
func (p *ProxyService) Serve() {
	_, serveArgs, _ := manet.DialArgs(p.proxyAddr)
	fmt.Println("proxy listening on ", serveArgs, p.dest)
	if p.dest != "" {
		http.ListenAndServe(serveArgs, p)
	}
}

// ServeHTTP implements the http.Handler interface. WARNING: This is the
// simplest approach to a proxy. Therefore, we do not do any of the things
// that should be done when implementing a reverse proxy (like handling
// headers correctly). For how to do it properly, see:
// https://golang.org/src/net/http/httputil/reverseproxy.go?s=3845:3920#L121
//
// ServeHTTP opens a stream to the dest peer for every HTTP request.
// Streams are multiplexed over single connections so, unlike connections
// themselves, they are cheap to create and dispose of.

// z: 1.本地127.0.0.1监听(http), 发送参数中写的http请求链接, 发送到p.dest, 在dest中发起http请求
// z: 1.这是一个http的监听回调方法
// z: 1.ProxyService对象中http.ListenAndServe(serveArgs, p), 在这个对象中定义改方法作为监听?
// z: 1. 第一个节点走这里, 第二个节点不走这里
func (p *ProxyService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("proxying request for %s to peer %s\n", r.URL, p.dest)

	stream, err := p.host.NewStream(context.Background(), p.dest, Protocol)

	if err != nil {
		log.Println(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// z: 3.如果出错就把传输通道关闭掉
	defer stream.Close()

	// r.Write() writes the HTTP request to the stream.
	// z: 4. stream作为参数, 却能把request写入stream?
	err = r.Write(stream)
	if err != nil {
		stream.Reset()
		log.Println(err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	// Now we read the response that was sent from the dest
	// peer

	// z: 5. 通过这个stream, 直接读取http请求结果
	// z: 5. stream 既是reader, 又是writer?
	buf := bufio.NewReader(stream)
	resp, err := http.ReadResponse(buf, r)
	if err != nil {
		stream.Reset()
		log.Println(err)
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	// Copy any headers
	for k, v := range resp.Header {
		for _, s := range v {
			w.Header().Add(k, s)
		}
	}

	// Write response status and headers
	// z: 6.w写入header返回
	w.WriteHeader(resp.StatusCode)

	// Finally copy the body
	// z: 6.w写入body返回
	io.Copy(w, resp.Body)
	// z: 6.Body是一个Read, 需要Close
	resp.Body.Close()
}

// addAddrToPeerstore parses a peer multiaddress and adds
// it to the given host's peerstore, so it knows how to
// contact it. It returns the peer ID of the remote peer.
func addAddrToPeerstore(h host.Host, addr string) peer.ID {
	maddr, err := ma.NewMultiaddr(addr)
	if err != nil {
		log.Println(err)
	}

	// Extract the peer ID from the multiaddr.
	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		log.Println(err)
	}

	// We have a peer ID and a targetAddr, so we add it to the peerstore
	// so LibP2P knows how to contact it
	h.Peerstore().AddAddrs(info.ID, info.Addrs, peerstore.PermanentAddrTTL)

	return info.ID
}

func createPrivKeyIfNoExist(privPath string) crypto.PrivKey {
	privByte, err := os.ReadFile(privPath)
	// base64.Encoding(privByte)

	var priv crypto.PrivKey
	if err != nil {
		priv, _, err = crypto.GenerateKeyPair(crypto.RSA, 2048)

		byte, err := crypto.MarshalPrivateKey(priv)
		if err != nil {
			log.Fatalln(err)
		}
		// 将字节数组写入文件
		// QmRYd6NaWkwf657X29N7oncdFxSenomNuPnpfSJmuMN8NE
		err = os.WriteFile(privPath, byte, 0644)
	} else {
		priv, _ = crypto.UnmarshalPrivateKey(privByte)
		if err != nil {
			log.Fatalln(err)
		}
	}
	return priv
}

func main() {

	priv := createPrivKeyIfNoExist("./mobile_private_key")
	p2pport := 12003

	dhts := convertPeers([]string{
		// "/ip4/127.0.0.1/tcp/12001/p2p/QmNp9m9D9mxrGrTeKLo3bah31msmgV1kz3VwhiSSdMCDYt",
		"/ip4/47.108.135.254/tcp/12001/p2p/QmNp9m9D9mxrGrTeKLo3bah31msmgV1kz3VwhiSSdMCDYt",
	})

	basicHost, err := makeRoutedHost(p2pport, dhts, priv)
	if err != nil {
		log.Panic(err)
	}
	httpPort := 9900

	target := "QmRYd6NaWkwf657X29N7oncdFxSenomNuPnpfSJmuMN8NE"

	destPeerID, err := peer.Decode(target)
	if err != nil {
		log.Fatalln(err)
	}

	proxyAddr, err := ma.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", httpPort))
	if err != nil {
		log.Fatalln(err)
	}

	if err != nil {
		log.Fatalln(err)
	}
	proxy := NewProxyService(basicHost, proxyAddr, destPeerID)

	// basicHost.SetStreamHandler(Protocol, streamHandler)

	proxy.Serve() // serve hangs forever

}

// makeRoutedHost creates a LibP2P host with a random peer ID listening on the
// given multiaddress. It will bootstrap using the provided PeerInfo.
func makeRoutedHost(listenPort int, bootstrapPeers []peer.AddrInfo, priv crypto.PrivKey) (host.Host, error) {

	opts := []libp2p.Option{
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", listenPort)),
		libp2p.Identity(priv),
		libp2p.DefaultTransports,
		libp2p.DefaultMuxers,
		libp2p.DefaultSecurity,
		libp2p.NATPortMap(),
	}

	ctx := context.Background()

	basicHost, err := libp2p.New(opts...)
	if err != nil {
		return nil, err
	}

	// Construct a datastore (needed by the DHT). This is just a simple, in-memory thread-safe datastore.
	dstore := dsync.MutexWrap(ds.NewMapDatastore())

	// Make the DHT
	dht := dht.NewDHT(ctx, basicHost, dstore)

	// Make the routed host
	routedHost := rhost.Wrap(basicHost, dht)

	// connect to the chosen ipfs nodes
	err = bootstrapConnect(ctx, routedHost, bootstrapPeers)
	if err != nil {
		return nil, err
	}

	// Bootstrap the host
	err = dht.Bootstrap(ctx)
	if err != nil {
		return nil, err
	}

	// Build host multiaddress
	hostAddr, _ := ma.NewMultiaddr(fmt.Sprintf("/ipfs/%s", routedHost.ID()))

	// Now we can build a full multiaddress to reach this host
	// by encapsulating both addresses:
	// addr := routedHost.Addrs()[0]
	addrs := routedHost.Addrs()
	fmt.Println("")
	log.Println("I can be reached at:")
	for _, addr := range addrs {
		log.Println(addr.Encapsulate(hostAddr))
	}
	return routedHost, nil
}

// This code is borrowed from the go-ipfs bootstrap process
func bootstrapConnect(ctx context.Context, ph host.Host, peers []peer.AddrInfo) error {
	if len(peers) < 1 {
		return errors.New("not enough bootstrap peers")
	}

	errs := make(chan error, len(peers))
	var wg sync.WaitGroup
	for _, p := range peers {

		// performed asynchronously because when performed synchronously, if
		// one `Connect` call hangs, subsequent calls are more likely to
		// fail/abort due to an expiring context.
		// Also, performed asynchronously for dial speed.

		wg.Add(1)
		go func(p peer.AddrInfo) {
			defer wg.Done()
			defer log.Println(ctx, "bootstrapDial", ph.ID(), p.ID)
			log.Printf("%s bootstrapping to %s", ph.ID(), p.ID)

			ph.Peerstore().AddAddrs(p.ID, p.Addrs, peerstore.PermanentAddrTTL)
			if err := ph.Connect(ctx, p); err != nil {
				log.Println(ctx, "bootstrapDialFailed", p.ID)
				log.Printf("failed to bootstrap with %v: %s", p.ID, err)
				errs <- err
				return
			}
			log.Println(ctx, "bootstrapDialSuccess", p.ID)
			log.Printf("bootstrapped with %v", p.ID)
		}(p)
	}
	wg.Wait()

	// our failure condition is when no connection attempt succeeded.
	// So drain the errs channel, counting the results.
	close(errs)
	count := 0
	var err error
	for err = range errs {
		if err != nil {
			count++
		}
	}
	if count == len(peers) {
		return fmt.Errorf("failed to bootstrap. %s", err)
	}
	return nil
}

func convertPeers(peers []string) []peer.AddrInfo {
	pinfos := make([]peer.AddrInfo, len(peers))
	for i, addr := range peers {
		maddr := ma.StringCast(addr)
		p, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			log.Fatalln(err)
		}
		pinfos[i] = *p
	}
	return pinfos
}
