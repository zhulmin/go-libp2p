package main

import (
	"bufio"
	"flag"
	"fmt"

	"log"
	"net/http"
	"strings"

	// We need to import libp2p's libraries that we use in this project.
	"github.com/libp2p/go-libp2p"
	
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	ma "github.com/multiformats/go-multiaddr"

		"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/transport"
	"github.com/libp2p/go-libp2p/p2p/net/swarm"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	tls "github.com/libp2p/go-libp2p/p2p/security/tls"
	quic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	webtransport "github.com/libp2p/go-libp2p/p2p/transport/webtransport"

	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

// Protocol defines the libp2p protocol that we will use for the libp2p proxy
// service that we are going to provide. This will tag the streams used for
// this service. Streams are multiplexed and their protocol tag helps
// libp2p handle them to the right handler functions.
const Protocol = "/proxy-example/0.0.1"

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

// streamHandler is our function to handle any libp2p-net streams that belong
// to our protocol. The streams should contain an HTTP request which we need
// to parse, make on behalf of the original node, and then write the response
// on the stream, before closing it.
// z: 11.stream handle处理进入的stream, 第二个节点要处理stream
// z: 11.第二个节点走这里, 真正的处理http请求,第一个节点不走这里, streamHandler是处理发送过来的stream并返回, 第一个发起节点不需要处理.
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

func main() {

	p2pport := flag.Int("l", 12000, "libp2p listen port")
	flag.Parse()

	priv, _, err := crypto.GenerateKeyPair(crypto.RSA, 2048)
	priv.GetPublic()
	priv.GetPublic().Raw()
	peerId, err := peer.IDFromPrivateKey(priv)

	options := []libp2p.Option{libp2p.Identity(priv),libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", *p2pport))}
	host, err := libp2p.New(options...)
	if err != nil {
		log.Fatalln(err)
	}

	// In this case we only need to make sure our host
	// knows how to handle incoming proxied requests from
	// another peer.

	//log.Println("/ip4/127.0.0.1/tcp/12000/ipfs/" + host.ID().String())

	// 报错会输入信息 503等
	// 注释以下代码 会报错500, 并且不会输出错误日志
	//接口输入
	// GET http://127.0.0.1:9900/ 500 (Internal Server Error)
	// 网页输出
	// failed to negotiate protocol: protocols not supported: [/proxy-example/0.0.1]

	//处理http请求并返回给源Peer
	_ = NewProxyService(host, nil, "")
	<-make(chan struct{}) // hang forever

}
