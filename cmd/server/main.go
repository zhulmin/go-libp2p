package main

import (
	"crypto/tls"
	"fmt"
	"log"
	"os"

	"github.com/caddyserver/certmagic"
	"github.com/libp2p/go-libp2p"
	ws "github.com/libp2p/go-ws-transport"
)

func main() {
	certmagic.DefaultACME.Agreed = true
	certmagic.DefaultACME.Email = "spam@spam.com"
	certmagic.DefaultACME.CA = certmagic.LetsEncryptStagingCA
	conf, err := certmagic.TLS([]string{os.Args[1]})
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		cache := certmagic.NewCache(certmagic.CacheOptions{})
		conf := certmagic.NewDefault()
		magic := certmagic.New(cache, *conf)
		tlsConfig := magic.TLSConfig()
		ln, err := tls.Listen("tcp", ":443", tlsConfig)
		if err != nil {
			log.Fatal(err)
		}
		_ = ln
	}()

	host, _ := libp2p.New(
		libp2p.Transport(ws.New, ws.WithTLSConfig(conf)),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/1234/wss"),
	)
	fmt.Println(host.ID())
	for _, addr := range host.Addrs() {
		fmt.Println(addr)
	}
	select {}
}
