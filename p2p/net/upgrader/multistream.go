package upgrader

import (
	"fmt"
	"net"
	"time"

	"github.com/libp2p/go-libp2p/core/network"

	mss "github.com/multiformats/go-multistream"
)

var DefaultNegotiateTimeout = time.Second * 60

type Multiplexer struct {
	ID          string
	StreamMuxer network.Multiplexer
}

type MsTransport interface {
	AddMuxer(path string, tpt network.Multiplexer)
	NegotiateMuxer(nc net.Conn, isServer bool) (*Multiplexer, error)
	GetTransportByKey(key string) (network.Multiplexer, bool)
}

type Transport struct {
	mux *mss.MultistreamMuxer

	tpts map[string]network.Multiplexer

	NegotiateTimeout time.Duration

	OrderPreference []string
}

func NewMsTransport() MsTransport {
	return &Transport{
		mux:              mss.NewMultistreamMuxer(),
		tpts:             make(map[string]network.Multiplexer),
		NegotiateTimeout: DefaultNegotiateTimeout,
	}
}

func (t *Transport) AddMuxer(path string, tpt network.Multiplexer) {
	t.mux.AddHandler(path, nil)
	t.tpts[path] = tpt
	t.OrderPreference = append(t.OrderPreference, path)
}

func (t *Transport) NegotiateMuxer(nc net.Conn, isServer bool) (*Multiplexer, error) {
	if t.NegotiateTimeout != 0 {
		if err := nc.SetDeadline(time.Now().Add(t.NegotiateTimeout)); err != nil {
			return nil, err
		}
	}

	var proto string
	if isServer {
		selected, _, err := t.mux.Negotiate(nc)
		if err != nil {
			return nil, err
		}
		proto = selected
	} else {
		selected, err := mss.SelectOneOf(t.OrderPreference, nc)
		if err != nil {
			return nil, err
		}
		proto = selected
	}

	if t.NegotiateTimeout != 0 {
		if err := nc.SetDeadline(time.Time{}); err != nil {
			return nil, err
		}
	}

	tpt, ok := t.tpts[proto]
	if !ok {
		return nil, fmt.Errorf("selected protocol we don't have a transport for")
	}
	return &Multiplexer{
		ID:          proto,
		StreamMuxer: tpt,
	}, nil
}

func (t *Transport) GetTransportByKey(key string) (network.Multiplexer, bool) {
	val, ok := t.tpts[key]
	return val, ok
}
