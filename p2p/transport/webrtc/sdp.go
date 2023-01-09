package libp2pwebrtc

import (
	"crypto"
	"fmt"
	"net"

	"github.com/multiformats/go-multihash"
)

const clientSDP string = `
v=0
o=- 0 0 IN %s %s
s=-
c=IN %s %s
t=0 0
m=application %d UDP/DTLS/SCTP webrtc-datachannel
a=mid:0
a=ice-ufrag:%s
a=ice-pwd:%s
a=fingerprint:sha-256 ba:78:16:bf:8f:01:cf:ea:41:41:40:de:5d:ae:22:23:b0:03:61:a3:96:17:7a:9c:b4:10:ff:61:f2:00:15:ad
a=setup:actpass
a=sctp-port:5000
a=max-message-size:16384
`

func renderClientSdp(addr *net.UDPAddr, ufrag string) string {
	ipVersion := "IP4"
	if addr.IP.To4() == nil {
		ipVersion = "IP6"
	}
	return fmt.Sprintf(
		clientSDP,
		ipVersion,
		addr.IP,
		ipVersion,
		addr.IP,
		addr.Port,
		ufrag,
		ufrag,
	)
}

const serverSDP string = `
v=0
o=- 0 0 IN %s %s
s=-
t=0 0
a=ice-lite
m=application %d UDP/DTLS/SCTP webrtc-datachannel
c=IN %s %s
a=mid:0
a=ice-options:ice2
a=ice-ufrag:%s
a=ice-pwd:%s
a=fingerprint:%s
a=setup:passive
a=sctp-port:5000
a=max-message-size:16384
a=candidate:1 1 UDP 1 %s %d typ host
`

func renderServerSdp(addr *net.UDPAddr, ufrag string, fingerprint *multihash.DecodedMultihash) string {
	ipVersion := "IP4"
	if addr.IP.To4() == nil {
		ipVersion = "IP6"
	}
	fp := fingerprintToSDP(fingerprint)
	return fmt.Sprintf(
		serverSDP,
		ipVersion,
		addr.IP,
		addr.Port,
		ipVersion,
		addr.IP,
		ufrag,
		ufrag,
		fp,
		addr.IP,
		addr.Port,
	)
}

func getSupportedSDPHash(code uint64) (crypto.Hash, bool) {
	switch code {
	case multihash.MD5:
		return crypto.MD5, true
	case multihash.SHA1:
		return crypto.SHA1, true
	case multihash.SHA3_224:
		return crypto.SHA3_224, true
	case multihash.SHA2_256:
		return crypto.SHA256, true
	case multihash.SHA3_384:
		return crypto.SHA3_384, true
	case multihash.SHA2_512:
		return crypto.SHA512, true
	}
	// default to sha256 but the dialer will fail
	// the multiaddr first
	return crypto.SHA256, false
}

func getSupportdSDPString(code uint64) string {
	switch code {
	case multihash.MD5:
		return "md5"
	case multihash.SHA1:
		return "sha-1"
	case multihash.SHA3_224:
		return "sha-224"
	case multihash.SHA2_256:
		return "sha-256"
	case multihash.SHA3_384:
		return "sha-384"
	case multihash.SHA2_512:
		return "sha-512"
	}
	return ""
}
