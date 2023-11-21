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
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	ma "github.com/multiformats/go-multiaddr"
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

func main() {

	privPath := "./nas_private_key"
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
	p2pport := 12002
	options := []libp2p.Option{libp2p.Identity(priv), libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", p2pport))}
	host, err := libp2p.New(options...)
	if err != nil {
		log.Fatalln(err)
	}
	for _, a := range host.Addrs() {
		///ip4/127.0.0.1/tcp/12000/p2p/QmddTrQXhA9AkCpXPTkcY7e22NK73TwkUms3a44DhTKJTD
		fmt.Printf("%s/p2p/%s\n", a, host.ID())
	}

	ctx := context.Background()

	dhts := convertPeers([]string{
		// "/ip4/127.0.0.1/tcp/12001/p2p/QmNp9m9D9mxrGrTeKLo3bah31msmgV1kz3VwhiSSdMCDYt",
		"/ip4/47.108.135.254/tcp/12001/p2p/QmNp9m9D9mxrGrTeKLo3bah31msmgV1kz3VwhiSSdMCDYt",
	})
	bootstrapConnect(ctx, host, dhts)

	host.SetStreamHandler(Protocol, streamHandler)

	<-make(chan struct{}) // hang forever

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
