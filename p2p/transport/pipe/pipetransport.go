package pipetransport

import (
	"context"
	"fmt"

	"sync"

	ic "github.com/libp2p/go-libp2p-crypto"
	peer "github.com/libp2p/go-libp2p-peer"
	tpt "github.com/libp2p/go-libp2p-transport"
	ma "github.com/multiformats/go-multiaddr"
)

type PipeTransport struct {
	mlistenchans *sync.RWMutex
	listenchans  map[string]chan *PipeConn

	id      peer.ID
	pubKey  ic.PubKey
	privKey ic.PrivKey
}

var _ tpt.Transport = (*PipeTransport)(nil)

func New(id peer.ID, pubKey ic.PubKey, privKey ic.PrivKey) *PipeTransport {
	return &PipeTransport{
		mlistenchans: new(sync.RWMutex),
		listenchans:  make(map[string]chan *PipeConn),
		id:           id,
		pubKey:       pubKey,
		privKey:      privKey,
	}
}

func (t *PipeTransport) closeListener(addr string) {
	t.mlistenchans.Lock()
	defer t.mlistenchans.Unlock()

	ch, ok := t.listenchans[addr]
	if !ok {
		return
	}
	close(ch)
	delete(t.listenchans, addr)
}

func (t *PipeTransport) CanDial(addr ma.Multiaddr) bool {
	protocols := addr.Protocols()
	return len(protocols) == 1 && protocols[0].Code == ma.P_P2P
}

func (t *PipeTransport) Protocols() []int {
	return []int{
		ma.P_P2P,
	}
}

func (t *PipeTransport) Proxy() bool {
	return false
}

func (t *PipeTransport) Dial(ctx context.Context, raddr ma.Multiaddr, p peer.ID) (tpt.Conn, error) {
	t.mlistenchans.RLock()
	defer t.mlistenchans.RUnlock()
	raddrStr := raddr.String()

	ch, ok := t.listenchans[raddrStr]
	if !ok {
		return nil, fmt.Errorf("no memorylistener for %s", raddrStr)
	}

	conn := NewPipeConn(t.id, raddr, t.pubKey, t.privKey, t)
	ch <- conn
	return conn, nil
}

func (t *PipeTransport) Listen(laddr ma.Multiaddr) (tpt.Listener, error) {
	t.mlistenchans.Lock()
	defer t.mlistenchans.Unlock()

	laddrStr := laddr.String()
	if _, ok := t.listenchans[laddrStr]; ok {
		return nil, fmt.Errorf("already listening on %s", laddrStr)
	}

	ch := make(chan *PipeConn)
	t.listenchans[laddrStr] = ch

	listener := NewPipeListener(laddr, ch, t)

	return listener, nil
}
