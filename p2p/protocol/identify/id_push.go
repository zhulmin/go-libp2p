package identify

import (
	"github.com/libp2p/go-libp2p-core/network"
)

// IDPush_1_1_0 supports signed peer records and larger message sizes.
const IDPush_1_1_0 = "/p2p/id/push/1.1.0"

// IDPush_1_0_0 is the protocol.ID of the legacy Identify push protocol. It sends full identify messages containing
// the current state of the peer but does NOT support support signed peer records and has smaller message sizes.
//
// It is in the process of being replaced by identify delta, which sends only diffs for better
// resource utilisation.
const IDPush_1_0_0 = "/ipfs/id/push/1.0.0"

// pushHandler handles incoming identify push streams. The behaviour is identical to the ordinary identify protocol.
func (ids *IDService) pushHandler(s network.Stream) {
	ids.handleIdentifyResponse(s)
}
