package memorytransport

import (
	"context"
	"fmt"

	"sync"

	peer "github.com/libp2p/go-libp2p-peer"
	tpt "github.com/libp2p/go-libp2p-transport"
	tptu "github.com/libp2p/go-libp2p-transport-upgrader"
	ma "github.com/multiformats/go-multiaddr"
)

type MemoryTransport struct {
	mlistenchans *sync.RWMutex
	listenchans  map[string]chan *MemoryConn

	upgrader *tptu.Upgrader
}

var _ tpt.Transport = (*MemoryTransport)(nil)

func New(u *tptu.Upgrader) *MemoryTransport {
	return &MemoryTransport{
		mlistenchans: new(sync.RWMutex),
		listenchans:  make(map[string]chan *MemoryConn),
		upgrader:     u,
	}
}

func (t *MemoryTransport) closeListener(addr string) {
	t.mlistenchans.Lock()
	defer t.mlistenchans.Unlock()

	ch, ok := t.listenchans[addr]
	if !ok {
		return
	}
	close(ch)
	delete(t.listenchans, addr)
}

func (t *MemoryTransport) CanDial(addr ma.Multiaddr) bool {
	protocols := addr.Protocols()
	return len(protocols) == 1 && protocols[0].Code == ma.P_P2P
}

func (t *MemoryTransport) Protocols() []int {
	return []int{
		ma.P_P2P,
	}
}

func (t *MemoryTransport) Proxy() bool {
	return false
}

func (t *MemoryTransport) Dial(ctx context.Context, raddr ma.Multiaddr, p peer.ID) (tpt.Conn, error) {
	t.mlistenchans.RLock()
	defer t.mlistenchans.RUnlock()
	raddrStr := raddr.String()

	ch, ok := t.listenchans[raddrStr]
	if !ok {
		return nil, fmt.Errorf("no memorylistener for %s", raddrStr)
	}

	connPair := NewMemoryConnPair()
	ch <- connPair.Listener

	return t.upgrader.UpgradeOutbound(ctx, t, connPair.Connector, p)
}

func (t *MemoryTransport) Listen(laddr ma.Multiaddr) (tpt.Listener, error) {
	t.mlistenchans.Lock()
	defer t.mlistenchans.Unlock()

	laddrStr := laddr.String()
	if _, ok := t.listenchans[laddrStr]; ok {
		return nil, fmt.Errorf("already listening on %s", laddrStr)
	}

	ch := make(chan *MemoryConn)
	t.listenchans[laddrStr] = ch

	listener := NewMemoryListener(laddr, ch, t)
	upgradedListener := t.upgrader.UpgradeListener(t, listener)

	return upgradedListener, nil
}
