package config

import (
	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/pnet"
	madns "github.com/multiformats/go-multiaddr-dns"
)

// TptC is the type for libp2p transport constructors. You probably won't ever
// implement this function interface directly. Instead, pass your transport
// constructor to TransportConstructor.
type TptC func(host.Host, network.Upgrader, pnet.PSK, connmgr.ConnectionGater, network.ResourceManager, *madns.Resolver) (network.Transport, error)

var transportArgTypes = argTypes

// TransportConstructor uses reflection to turn a function that constructs a
// transport into a TptC.
//
// You can pass either a constructed transport (something that implements
// `network.Transport`) or a function that takes any of:
//
// * The local peer ID.
// * A transport connection upgrader.
// * A private key.
// * A public key.
// * A Host.
// * A Network.
// * A Peerstore.
// * An address filter.
// * A security transport.
// * A stream multiplexer transport.
// * A private network protection key.
// * A connection gater.
//
// And returns a type implementing network.Transport and, optionally, an error
// (as the second argument).
func TransportConstructor(tpt interface{}, opts ...interface{}) (TptC, error) {
	// Already constructed?
	if t, ok := tpt.(network.Transport); ok {
		return func(_ host.Host, _ network.Upgrader, _ pnet.PSK, _ connmgr.ConnectionGater, _ network.ResourceManager, _ *madns.Resolver) (network.Transport, error) {
			return t, nil
		}, nil
	}
	ctor, err := makeConstructor(tpt, transportType, transportArgTypes, opts...)
	if err != nil {
		return nil, err
	}
	return func(h host.Host, u network.Upgrader, psk pnet.PSK, cg connmgr.ConnectionGater, rcmgr network.ResourceManager, resolver *madns.Resolver) (network.Transport, error) {
		t, err := ctor(h, u, psk, cg, rcmgr, resolver)
		if err != nil {
			return nil, err
		}
		return t.(network.Transport), nil
	}, nil
}

func makeTransports(h host.Host, u network.Upgrader, cg connmgr.ConnectionGater, psk pnet.PSK, rcmgr network.ResourceManager, resolver *madns.Resolver, tpts []TptC) ([]network.Transport, error) {
	transports := make([]network.Transport, len(tpts))
	for i, tC := range tpts {
		t, err := tC(h, u, psk, cg, rcmgr, resolver)
		if err != nil {
			return nil, err
		}
		transports[i] = t
	}
	return transports, nil
}
