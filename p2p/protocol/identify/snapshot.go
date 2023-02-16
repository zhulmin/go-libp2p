package identify

import (
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/core/record"

	ma "github.com/multiformats/go-multiaddr"
)

type identifySnapshot struct {
	Sequence  uint64
	Protocols []protocol.ID
	Addrs     []ma.Multiaddr
	Record    *record.Envelope
}

// Equal tells if two snapshots are identical.
// It *does not* check for equality of the sequence number.
func (s identifySnapshot) Equal(s2 *identifySnapshot) bool {
	if len(s.Protocols) != len(s2.Protocols) || len(s.Addrs) != len(s2.Addrs) {
		return false
	}
	if (s.Record == nil && s2.Record != nil) || (s.Record != nil && s2.Record == nil) {
		return false
	}
	if s.Record != nil && !s.Record.Equal(s2.Record) {
		return false
	}
	for i, p := range s.Protocols {
		if p != s2.Protocols[i] {
			return false
		}
	}
	for i, a := range s.Addrs {
		if !a.Equal(s2.Addrs[i]) {
			return false
		}
	}
	return true
}
