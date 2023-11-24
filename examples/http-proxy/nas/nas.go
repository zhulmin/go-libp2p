package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"log"
	"net/http"
	"strings"

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
	ma "github.com/multiformats/go-multiaddr"

	rhost "github.com/libp2p/go-libp2p/p2p/host/routed"
)

const Protocol = "/yole.app/0.0.1"

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
	// req.URL.Host = req.Host
	req.URL.Host = "10.0.0.15"

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

	resp.Write(stream)
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

	priv := createPrivKeyIfNoExist("./nas_private_key")
	p2pport := 12002

	dhts := convertPeers([]string{
		// "/ip4/127.0.0.1/tcp/12001/p2p/QmNp9m9D9mxrGrTeKLo3bah31msmgV1kz3VwhiSSdMCDYt",
		"/ip4/47.108.135.254/tcp/12001/p2p/QmNp9m9D9mxrGrTeKLo3bah31msmgV1kz3VwhiSSdMCDYt",
	})
	basicHost, err := makeRoutedHost(p2pport, dhts, priv)
	if err != nil {
		log.Panic(err)
	}
	// options := []libp2p.Option{libp2p.Identity(priv), libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", p2pport))}
	// basicHost, err := libp2p.New(options...)
	// if err != nil {
	// 	log.Fatalln(err)
	// }

	// // Construct a datastore (needed by the DHT). This is just a simple, in-memory thread-safe datastore.
	// dstore := dsync.MutexWrap(ds.NewMapDatastore())

	// // Make the DHT
	// dht := dht.NewDHT(ctx, basicHost, dstore)

	// // Make the routed host
	// routedHost := rhost.Wrap(basicHost, dht)

	// // connect to the chosen ipfs nodes
	// err = bootstrapConnect(ctx, routedHost, dhts)
	// if err != nil {
	// 	log.Panic(err)
	// }

	// // Bootstrap the host
	// err = dht.Bootstrap(ctx)
	// if err != nil {
	// 	log.Panic(err)
	// }

	basicHost.SetStreamHandler(Protocol, streamHandler)

	<-make(chan struct{}) // hang forever

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
	log.Printf("\n")
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
