package params

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/transport"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multibase"
	"github.com/multiformats/go-multihash"
)

type TransportWithCerthash interface {
	transport.Transport
	// DialWithCerthash is the same as transport.Transport's dial, but has a certhash parameter
	DialWithCerthash(ctx context.Context, raddr ma.Multiaddr, p peer.ID, certhashes []multihash.DecodedMultihash) (transport.CapableConn, error)
}

type Certhash[T TransportWithCerthash] struct {
	InnerTransport T
}

var _ transport.Transport = &Certhash[TransportWithCerthash]{}

// CanDial implements transport.Transport
func (c *Certhash[T]) CanDial(addr ma.Multiaddr) bool {
	rest, head := ma.SplitLast(addr)
	if head.Protocol().Code == ma.P_CERTHASH {
		return c.InnerTransport.CanDial(rest)
	}
	return false

}

// Dial implements transport.Transport
func (c *Certhash[T]) Dial(ctx context.Context, raddr ma.Multiaddr, p peer.ID) (transport.CapableConn, error) {
	var certhashesStr []string
	rest, head := ma.SplitLast(raddr)
	for head.Protocol().Code == ma.P_CERTHASH {
		certhashesStr = append(certhashesStr, head.Value())
		rest, head = ma.SplitLast(rest)
	}

	// put back the last thing we popped
	rest = ma.Join(rest, head)

	certHashes := make([]multihash.DecodedMultihash, 0, len(certhashesStr))
	for _, s := range certhashesStr {
		_, ch, err := multibase.Decode(s)
		if err != nil {
			return nil, fmt.Errorf("failed to multibase-decode certificate hash: %w", err)
		}
		dh, err := multihash.Decode(ch)
		if err != nil {
			return nil, fmt.Errorf("failed to multihash-decode certificate hash: %w", err)
		}
		certHashes = append(certHashes, *dh)
	}
	return c.InnerTransport.DialWithCerthash(ctx, rest, p, certHashes)
}

// Listen implements transport.Transport
func (c *Certhash[T]) CanListen(laddr ma.Multiaddr) bool {
	rest, head := ma.SplitLast(laddr)
	for head.Protocol().Code == ma.P_CERTHASH {
		rest, head = ma.SplitLast(rest)
	}
	for _, p := range c.InnerTransport.Protocols() {
		if head.Protocol().Code == p {
			return true
		}

	}
	return false
}

// Listen implements transport.Transport
func (c *Certhash[T]) Listen(laddr ma.Multiaddr) (transport.Listener, error) {
	rest, head := ma.SplitLast(laddr)
	for head.Protocol().Code == ma.P_CERTHASH {
		rest, head = ma.SplitLast(rest)
	}
	return c.InnerTransport.Listen(rest)
}

// Protocols implements transport.Transport
func (c *Certhash[T]) Protocols() []int {
	return []int{ma.P_CERTHASH}
}

// Proxy implements transport.Transport
func (c *Certhash[T]) Proxy() bool {
	return c.InnerTransport.Proxy()
}
