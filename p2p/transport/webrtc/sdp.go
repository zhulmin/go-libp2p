package libp2pwebrtc

import (
	"bytes"
	"crypto"
	"fmt"
	"html/template"
	"net"
	"strings"

	"github.com/multiformats/go-multihash"
)

var clientTpl *template.Template

const clientTplStr = `
v=0
o=- 0 0 IN {{.IpVersion}} {{.Addr.IP}}
s=-
c=IN {{.IpVersion}} {{.Addr.IP}}
t=0 0
m=application {{.Addr.Port}} UDP/DTLS/SCTP webrtc-datachannel
a=mid:0
a=ice-options:trickle
a=ice-ufrag:{{.Ufrag}}
a=ice-pwd:{{.Ufrag}}
a=fingerprint:sha-256 ba:78:16:bf:8f:01:cf:ea:41:41:40:de:5d:ae:22:23:b0:03:61:a3:96:17:7a:9c:b4:10:ff:61:f2:00:15:ad
a=setup:actpass
a=sctp-port:5000
a=max-message-size:16384
`

func init() {

	clientTpl = template.Must(template.New("client").Parse(clientTplStr))
}

// clientSDP describes an SDP format string which can be used
// to infer a client's SDP offer from the incoming STUN message.
// Note: ice-trickle is enabled by default on Pion, but in this
// case it does not matter since we cannot exchange SDP or candidates.
const clientSDP string = `
v=0
o=- 0 0 IN %[1]s %[2]s
s=-
c=IN %[1]s %[2]s
t=0 0
m=application %[3]d UDP/DTLS/SCTP webrtc-datachannel
a=mid:0
a=ice-options:trickle
a=ice-ufrag:%[4]s
a=ice-pwd:%[4]s
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
		addr.Port,
		ufrag,
	)
}

func renderClientSdp2(addr *net.UDPAddr, ufrag string) string {
	ipVersion := "IP4"
	if addr.IP.To4() == nil {
		ipVersion = "IP6"
	}
	var tpl bytes.Buffer
	clientTpl.Execute(&tpl, struct {
		IpVersion string
		Addr      net.Addr
		Ufrag     string
	}{ipVersion, addr, ufrag})
	return tpl.String()
}

// serverSDP defines an SDP format string used by a dialer
// to infer the SDP answer of a server based on the provided
// multiaddr, and the locally set ICE credentials.
const serverSDP string = `
v=0
o=- 0 0 IN %[1]s %[2]s
s=-
t=0 0
a=ice-lite
m=application %[3]d UDP/DTLS/SCTP webrtc-datachannel
c=IN %[1]s %[2]s
a=mid:0
a=ice-options:ice2
a=ice-ufrag:%[4]s
a=ice-pwd:%[4]s
a=fingerprint:%[5]s
a=setup:passive
a=sctp-port:5000
a=max-message-size:16384
a=candidate:1 1 UDP 1 %[2]s %[3]d typ host
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
		ufrag,
		fp,
	)
}

// getSupportedSDPHash converts a multihash code to the
// corresponding crypto.Hash for supported protocols. If a
// crypto.Hash cannot be found, it returns `(crypto.SHA256, false)`
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
	default:
		return crypto.SHA256, false
	}
}

// getSupportedSDPString converts a multihash code
// to a string format recognised by pion for fingerprint
// algorithms
func getSupportedSDPString(code uint64) string {
	hash, ok := getSupportedSDPHash(code)
	if ok {
		return strings.ToLower(hash.String())
	}
	return ""
}
