package tor

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"strings"

	"github.com/cretz/bine/tor"
	"github.com/libp2p/go-doh-resolver"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/sec"
	"github.com/libp2p/go-libp2p/core/transport"
	blankhost "github.com/libp2p/go-libp2p/p2p/host/blank"
	"github.com/libp2p/go-libp2p/p2p/host/eventbus"
	"github.com/libp2p/go-libp2p/p2p/host/peerstore/pstoremem"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/net/swarm"
	"github.com/libp2p/go-libp2p/p2p/net/upgrader"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	ma "github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
	"golang.org/x/crypto/sha3"
)

type Host struct {
	*blankhost.BlankHost
	tor *tor.Tor
}

var _ host.Host = (*Host)(nil)

type HostSettings struct {
	PeerKey crypto.PrivKey
}

type HostOption func(*HostSettings) error

var RandomIdentity HostOption = func(o *HostSettings) error {
	priv, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return err
	}
	o.PeerKey = priv
	return nil
}

func NewHost(opts ...HostOption) (*Host, error) {
	s := &HostSettings{}
	def := []HostOption{RandomIdentity}
	allOpts := append(opts, def...)
	for _, o := range allOpts {
		err := o(s)
		if err != nil {
			return nil, err
		}
	}
	pid, err := peer.IDFromPublicKey(s.PeerKey.GetPublic())
	if err != nil {
		return nil, err
	}
	ps, err := pstoremem.NewPeerstore()
	if err != nil {
		return nil, err
	}
	if err := ps.AddPrivKey(pid, s.PeerKey); err != nil {
		return nil, err
	}
	if err := ps.AddPubKey(pid, s.PeerKey.GetPublic()); err != nil {
		return nil, err
	}

	t, err := startTor()
	if err != nil {
		return nil, err
	}
	dialer, err := t.Dialer(context.Background(), &tor.DialConf{})
	if err != nil {
		t.Close()
		return nil, err
	}
	bus := eventbus.NewBus()
	res, err := doh.NewResolver("https://cloudflare-dns.com/dns-query", doh.WithDialer(dialer))
	if err != nil {
		t.Close()
		return nil, err
	}
	resolver, err := madns.NewResolver(madns.WithDefaultResolver(res))
	if err != nil {
		t.Close()
		return nil, err
	}
	sw, err := swarm.NewSwarm(pid, ps, bus, swarm.WithMultiaddrResolver(resolver))
	if err != nil {
		t.Close()
		return nil, err
	}

	upg, err := newUpgrader(pid, s.PeerKey)
	if err != nil {
		t.Close()
		return nil, err
	}
	tpt, err := NewTransport(t, upg, nil)
	if err != nil {
		t.Close()
		return nil, err
	}

	if err := sw.AddTransport(tpt); err != nil {
		t.Close()
		return nil, err
	}

	bh := blankhost.NewBlankHost(sw, blankhost.WithEventBus(bus))
	return &Host{BlankHost: bh, tor: t}, nil
}

func startTor() (*tor.Tor, error) {
	return tor.Start(context.Background(), nil)
}

func newUpgrader(pid peer.ID, pk crypto.PrivKey) (transport.Upgrader, error) {
	st, err := noise.New(noise.ID, pk, []upgrader.StreamMuxer{{ID: yamux.ID, Muxer: yamux.DefaultTransport}})
	if err != nil {
		return nil, err
	}
	return upgrader.New(
		[]sec.SecureTransport{st},
		[]upgrader.StreamMuxer{{ID: yamux.ID, Muxer: yamux.DefaultTransport}}, nil, nil, nil)
}

func (h *Host) Close() error {
	h.BlankHost.Close()
	h.tor.Close()
	return nil
}

func GenOnionAddress(pubKey *crypto.Ed25519PublicKey) (ma.Multiaddr, error) {
	raw, err := pubKey.Raw()
	if err != nil {
		return nil, err
	}
	h := sha3.New256()
	h.Write([]byte(".onion checksum"))
	h.Write(raw)
	h.Write([]byte{0x03})
	sum := h.Sum(nil)[:2]
	allbytes := make([]byte, 0)
	allbytes = append(allbytes, raw...)
	allbytes = append(allbytes, sum...)
	allbytes = append(allbytes, 0x03)
	addr := ma.StringCast("/onion3/" + strings.ToLower(base32.StdEncoding.EncodeToString(allbytes)) + ":1337")
	return addr, nil
}
