package swarm

import (
	"fmt"

	"github.com/libp2p/go-libp2p/core/transport"

	ma "github.com/multiformats/go-multiaddr"
)

// TransportForDialing retrieves the appropriate transport for dialing the given
// multiaddr.
func (s *Swarm) TransportForDialing(a ma.Multiaddr) transport.Transport {
	protocols := a.Protocols()
	if len(protocols) == 0 {
		return nil
	}

	s.transports.RLock()
	defer s.transports.RUnlock()

	if len(s.transports.m) == 0 {
		// make sure we're not just shutting down.
		if s.transports.m != nil {
			log.Error("you have no transports configured")
		}
		return nil
	}
	if isRelayAddr(a) {
		tpts := s.transports.m[ma.P_CIRCUIT]
		if len(tpts) == 0 {
			return nil
		}
		return tpts[0]
	}

	selectedTpts := s.transports.m[protocols[len(protocols)-1].Code]
	for _, t := range selectedTpts {
		if t.CanDial(a) {
			return t
		}
	}
	return nil
}

// TransportForListening retrieves the appropriate transport for listening on
// the given multiaddr.
func (s *Swarm) TransportForListening(a ma.Multiaddr) transport.Transport {
	protocols := a.Protocols()
	if len(protocols) == 0 {
		return nil
	}

	s.transports.RLock()
	defer s.transports.RUnlock()
	if len(s.transports.m) == 0 {
		// make sure we're not just shutting down.
		if s.transports.m != nil {
			log.Error("you have no transports configured")
		}
		return nil
	}

	selectedTpts := s.transports.m[protocols[len(protocols)-1].Code]
	if len(selectedTpts) == 0 {
		return nil
	}

	selected := selectedTpts[0]
	for _, tpt := range selectedTpts {
		if canListen, ok := tpt.(transport.CanListen); ok && canListen.CanListen(a) {
			selected = tpt
			break
		} else {
			break
		}
	}

	// TODO this is a hacky way to support p2p circuits.
	// Instead this should be a proxy transport that wraps other transports
	for _, p := range protocols {
		transports, ok := s.transports.m[p.Code]
		if !ok || len(transports) == 0 {
			continue
		}
		transport := transports[0]
		if transport.Proxy() {
			selected = transport
		}
	}
	return selected
}

// AddTransport adds a transport to this swarm.
//
// Satisfies the Network interface from go-libp2p-transport.
func (s *Swarm) AddTransport(t transport.Transport) error {
	protocols := t.Protocols()

	if len(protocols) == 0 {
		return fmt.Errorf("useless transport handles no protocols: %T", t)
	}

	s.transports.Lock()
	defer s.transports.Unlock()
	if s.transports.m == nil {
		return ErrSwarmClosed
	}
	for _, p := range protocols {
		s.transports.m[p] = append(s.transports.m[p], t)
	}
	return nil
}
