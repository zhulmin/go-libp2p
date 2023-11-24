package main

import (
	"context"
	"fmt"
	"log"
	"os"

	// We need to import libp2p's libraries that we use in this project.
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"

	// We need to import libp2p's libraries that we use in this project.

	ma "github.com/multiformats/go-multiaddr"

	rhost "github.com/libp2p/go-libp2p/p2p/host/routed"
)

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

	// Parse some flags

	priv := createPrivKeyIfNoExist("./dht_private_key")
	p2pport := 12001

	basicHost, err := makeRoutedHost(p2pport, priv)

	_, err = relay.New(basicHost)
	if err != nil {
		log.Printf("Failed to instantiate the relay: %v", err)
		return
	}

	for _, a := range basicHost.Addrs() {
		///ip4/127.0.0.1/tcp/12000/p2p/QmddTrQXhA9AkCpXPTkcY7e22NK73TwkUms3a44DhTKJTD
		fmt.Printf("%s/p2p/%s\n", a, basicHost.ID())
	}

	<-make(chan struct{})

}

// makeRoutedHost creates a LibP2P host with a random peer ID listening on the
// given multiaddress. It will bootstrap using the provided PeerInfo.
func makeRoutedHost(listenPort int, priv crypto.PrivKey) (host.Host, error) {

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
	// dstore := dsync.MutexWrap(ds.NewMapDatastore())

	// Make the DHT
	// dht := dht.NewDHT(ctx, basicHost, dstore)
	kademliaDHT, err := dht.New(ctx, basicHost, dht.Mode(dht.ModeAutoServer))

	// Make the routed host
	routedHost := rhost.Wrap(basicHost, kademliaDHT)

	// Bootstrap the host
	err = kademliaDHT.Bootstrap(ctx)
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
