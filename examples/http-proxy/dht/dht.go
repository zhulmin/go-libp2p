package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"

	// We need to import libp2p's libraries that we use in this project.
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
)

func main() {

	// Parse some flags
	privPath := "./dht_private_key"
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
	port := 12001
	options := []libp2p.Option{libp2p.Identity(priv),
		// libp2p.ListenAddrStrings("/ip4/0.0.0.0/udp/" + strconv.Itoa(port),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/" + strconv.Itoa(port))}

	host, err := libp2p.New(
		options...,
	)
	if err != nil {
		log.Panic(err)
	}
	ctx := context.Background()
	kademliaDHT, err := dht.New(ctx, host)
	if err = kademliaDHT.Bootstrap(ctx); err != nil {
		panic(err)
	}
	for _, a := range host.Addrs() {
		///ip4/127.0.0.1/tcp/12000/p2p/QmddTrQXhA9AkCpXPTkcY7e22NK73TwkUms3a44DhTKJTD
		fmt.Printf("%s/p2p/%s\n", a, host.ID())
	}

	<-make(chan struct{})

}
