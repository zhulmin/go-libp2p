package main

import (
	"fmt"
	"log"
	"os"

	"github.com/caddyserver/certmagic"
	"github.com/libp2p/go-libp2p"
	ws "github.com/libp2p/go-ws-transport"
)

func main() {
	certmagic.DefaultACME.CA = certmagic.LetsEncryptStagingCA
	conf, err := certmagic.TLS([]string{os.Args[1]})
	if err != nil {
		log.Fatal(err)
	}
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
