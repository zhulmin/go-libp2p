package holepunch

import (
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

// WithAddrFilter is a Service option that enables multiaddress filtering.
// It allows to only send a subset of observed addresses to the remote
// peer. E.g., only announce TCP or QUIC multi addresses instead of both.
// It also allows to only consider a subset of received multi addresses
// that remote peers announced to us.
// Theoretically, this API also allows to add multi addresses in both cases.
func WithAddrFilter(maf AddrFilter) Option {
	return func(hps *Service) error {
		hps.filter = maf
		return nil
	}
}

// AddrFilter defines the interface for the multi address filtering.
type AddrFilter interface {
	// FilterLocal is a function that filters the multi addresses that we send to the remote peer.
	FilterLocal(remoteID peer.ID, maddrs []ma.Multiaddr) []ma.Multiaddr
	// FilterRemote is a function that filters the multi addresses which we received from the remote peer.
	FilterRemote(remoteID peer.ID, maddrs []ma.Multiaddr) []ma.Multiaddr
}

// DefaultAddrFilter is the default address filtering logic. It strips
// all relayed multi addresses from both the locally observed addresses
// and received remote addresses.
type DefaultAddrFilter struct{}

func (d DefaultAddrFilter) FilterLocal(remoteID peer.ID, maddrs []ma.Multiaddr) []ma.Multiaddr {
	return removeRelayAddrs(maddrs)
}

func (d DefaultAddrFilter) FilterRemote(remoteID peer.ID, maddrs []ma.Multiaddr) []ma.Multiaddr {
	return removeRelayAddrs(maddrs)
}
