module github.com/libp2p/go-libp2p

require (
	github.com/benbjohnson/clock v1.0.2
	github.com/davecgh/go-spew v1.1.1
	github.com/gogo/protobuf v1.3.1
	github.com/gorilla/websocket v1.4.2
	github.com/ipfs/go-cid v0.0.5
	github.com/ipfs/go-detect-race v0.0.1
	github.com/ipfs/go-ipfs-util v0.0.1
	github.com/ipfs/go-log v1.0.4
	github.com/ipfs/go-log/v2 v2.0.5
	github.com/jbenet/go-cienv v0.1.0
	github.com/jbenet/goprocess v0.1.4
	github.com/libp2p/go-conn-security-multistream v0.2.0
	github.com/libp2p/go-eventbus v0.1.1-0.20200417061519-63254f6c0da4
	github.com/libp2p/go-libp2p-autonat v0.2.2
	github.com/libp2p/go-libp2p-blankhost v0.1.4
	github.com/libp2p/go-libp2p-circuit v0.2.3
	github.com/libp2p/go-libp2p-core v0.5.7
	github.com/libp2p/go-libp2p-discovery v0.3.0
	github.com/libp2p/go-libp2p-introspector v0.0.5-0.20200424091613-bee588f5a69a
	github.com/libp2p/go-libp2p-loggables v0.1.0
	github.com/libp2p/go-libp2p-mplex v0.2.3
	github.com/libp2p/go-libp2p-nat v0.0.6
	github.com/libp2p/go-libp2p-netutil v0.1.0
	github.com/libp2p/go-libp2p-peerstore v0.2.4
	github.com/libp2p/go-libp2p-secio v0.2.2
	github.com/libp2p/go-libp2p-swarm v0.2.4-0.20200424120434-6a7bedc68235
	github.com/libp2p/go-libp2p-testing v0.1.1
	github.com/libp2p/go-libp2p-tls v0.1.3
	github.com/libp2p/go-libp2p-transport-upgrader v0.3.0
	github.com/libp2p/go-libp2p-yamux v0.2.8
	github.com/libp2p/go-maddr-filter v0.0.5
	github.com/libp2p/go-stream-muxer-multistream v0.3.0
	github.com/libp2p/go-tcp-transport v0.2.0
	github.com/libp2p/go-ws-transport v0.3.0
	github.com/multiformats/go-multiaddr v0.2.2
	github.com/multiformats/go-multiaddr-dns v0.2.0
	github.com/multiformats/go-multiaddr-net v0.1.5
	github.com/multiformats/go-multistream v0.1.1
	github.com/stretchr/testify v1.6.0
	github.com/whyrusleeping/mdns v0.0.0-20190826153040-b9b60ed33aa9
	go.uber.org/zap v1.14.1
	golang.org/x/net v0.0.0-20190923162816-aa69164e4478
)

replace github.com/libp2p/go-libp2p-core => ../go-libp2p-core

replace github.com/libp2p/go-libp2p-swarm => ../go-libp2p-swarm

replace github.com/libp2p/go-eventbus => ../go-eventbus

go 1.12
