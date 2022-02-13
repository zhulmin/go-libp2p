package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/libp2p/go-libp2p"
	ws "github.com/libp2p/go-ws-transport"
)

func main() {
	cert, err := generateCert()
	if err != nil {
		log.Fatal(err)
	}
	conf := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}
	host, err := libp2p.New(
		libp2p.Transport(ws.New, ws.WithTLSConfig(conf)),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/1234/ws", "/ip4/0.0.0.0/tcp/1234/wss"),
	)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(host.ID())
	for _, addr := range host.Addrs() {
		fmt.Println(addr)
	}
	select {}
}

func generateCert() (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{},
		SignatureAlgorithm:    x509.SHA256WithRSA,
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour), // valid for an hour
		BasicConstraintsValid: true,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, priv.Public(), priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{
		PrivateKey:  priv,
		Certificate: [][]byte{certDER},
	}, nil
}
