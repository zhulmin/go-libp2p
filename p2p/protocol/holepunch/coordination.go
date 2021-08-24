package holepunch

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/libp2p/go-libp2p/p2p/protocol/holepunch/pb"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"

	logging "github.com/ipfs/go-log"
	"github.com/libp2p/go-msgio/protoio"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// Protocol is the libp2p protocol for Hole Punching.
const Protocol protocol.ID = "/libp2p/dcutr"

// StreamTimeout is the timeout for the hole punch protocol stream.
var StreamTimeout = 1 * time.Minute

// TODO Should we have options for these ?
const (
	maxMsgSize  = 4 * 1024 // 4K
	dialTimeout = 5 * time.Second
	maxRetries  = 3
	retryWait   = 2 * time.Second
)

var (
	log = logging.Logger("p2p-holepunch")
)

// The HolePunchService is used to make direct connections with a peer via hole-punching.
type HolePunchService struct {
	ctx       context.Context
	ctxCancel context.CancelFunc

	ids  *identify.IDService
	host host.Host

	tracer *Tracer

	refCount sync.WaitGroup
	counter  int32 // to be used as an atomic. How many hole punch requests we have responded to

	// active hole punches for deduplicating
	activeMx sync.Mutex
	active   map[peer.ID]struct{}
}

type Option func(*HolePunchService) error

// NewHolePunchService creates a new service that can be used for hole punching
func NewHolePunchService(h host.Host, ids *identify.IDService, opts ...Option) (*HolePunchService, error) {
	if ids == nil {
		return nil, errors.New("identify service can't be nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	hs := &HolePunchService{
		ctx:       ctx,
		ctxCancel: cancel,
		host:      h,
		ids:       ids,
		active:    make(map[peer.ID]struct{}),
	}

	for _, opt := range opts {
		if err := opt(hs); err != nil {
			cancel()
			return nil, err
		}
	}

	h.SetStreamHandler(Protocol, hs.handleNewStream)
	h.Network().Notify((*netNotifiee)(hs))
	return hs, nil
}

// Close closes the Hole Punch Service.
func (hs *HolePunchService) Close() error {
	hs.ctxCancel()
	hs.refCount.Wait()
	return nil
}

// initiateHolePunch opens a new hole punching coordination stream,
// exchanges the addresses and measures the RTT.
func (hs *HolePunchService) initiateHolePunch(rp peer.ID) ([]ma.Multiaddr, time.Duration, error) {
	hpCtx := network.WithUseTransient(hs.ctx, "hole-punch")
	sCtx := network.WithNoDial(hpCtx, "hole-punch")
	str, err := hs.host.NewStream(sCtx, rp, Protocol)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open hole-punching stream with peer %s: %w", rp, err)
	}
	defer str.Close()
	str.SetDeadline(time.Now().Add(StreamTimeout))

	log.Infof("will attempt hole punch with peer %s", rp.Pretty())
	w := protoio.NewDelimitedWriter(str)

	// send a CONNECT and start RTT measurement.
	msg := &pb.HolePunch{
		Type:     pb.HolePunch_CONNECT.Enum(),
		ObsAddrs: addrsToBytes(hs.ids.OwnObservedAddrs()),
	}

	start := time.Now()
	if err := w.WriteMsg(msg); err != nil {
		str.Reset()
		return nil, 0, err
	}

	// wait for a CONNECT message from the remote peer
	rd := protoio.NewDelimitedReader(str, maxMsgSize)
	msg.Reset()
	if err := rd.ReadMsg(msg); err != nil {
		str.Reset()
		return nil, 0, fmt.Errorf("failed to read CONNECT message from remote peer: %w", err)
	}
	rtt := time.Since(start)

	if t := msg.GetType(); t != pb.HolePunch_CONNECT {
		str.Reset()
		return nil, 0, fmt.Errorf("expect CONNECT message, got %s", t)
	}

	addrs := addrsFromBytes(msg.ObsAddrs)

	msg.Reset()
	msg.Type = pb.HolePunch_SYNC.Enum()
	if err := w.WriteMsg(msg); err != nil {
		str.Reset()
		return nil, 0, fmt.Errorf("failed to send SYNC message for hole punching: %w", err)
	}
	return addrs, rtt, nil
}

// attempts to make a direct connection with the remote peer of `relayConn` by co-ordinating a hole punch over
// the given relay connection `relayConn`.
func (hs *HolePunchService) HolePunch(rp peer.ID) error {
	// short-circuit check to see if we already have a direct connection
	for _, c := range hs.host.Network().ConnsToPeer(rp) {
		if !isRelayAddress(c.RemoteMultiaddr()) {
			return nil
		}
	}

	// short-circuit hole punching if a direct dial works.
	// attempt a direct connection ONLY if we have a public address for the remote peer
	for _, a := range hs.host.Peerstore().Addrs(rp) {
		if manet.IsPublicAddr(a) && !isRelayAddress(a) {
			forceDirectConnCtx := network.WithForceDirectDial(hs.ctx, "hole-punching")
			dialCtx, cancel := context.WithTimeout(forceDirectConnCtx, dialTimeout)

			tstart := time.Now()
			err := hs.host.Connect(dialCtx, peer.AddrInfo{ID: rp})
			dt := time.Since(tstart)
			cancel()

			if err != nil {
				hs.tracer.DirectDialFailed(rp, dt, err)
				break
			}
			hs.tracer.DirectDialSuccessful(rp, dt)
			log.Debugf("direct connection to peer %s successful, no need for a hole punch", rp.Pretty())
			return nil
		}
	}

	// hole punch
	for i := 0; i < maxRetries; i++ {
		addrs, rtt, err := hs.initiateHolePunch(rp)
		if err != nil {
			hs.handlerError(rp, err)
			return err
		}
		synTime := rtt / 2
		log.Debugf("peer RTT is %s; starting hole punch in %s", rtt, synTime)

		// wait for sync to reach the other peer and then punch a hole for it in our NAT
		// by attempting a connect to it.
		timer := time.NewTimer(synTime)
		select {
		case <-timer.C:
			pi := peer.AddrInfo{
				ID:    rp,
				Addrs: addrs,
			}
			start := time.Now()
			hs.tracer.StartHolePunch(rp, addrs, rtt)
			err := hs.holePunchConnect(pi, i+1)
			dt := time.Since(start)
			hs.tracer.EndHolePunch(rp, dt, err)
			if err == nil {
				log.Infof("hole punching with %s successful after %s", rp, dt)
				return nil
			}
		case <-hs.ctx.Done():
			timer.Stop()
			return hs.ctx.Err()
		}
	}
	return fmt.Errorf("all retries for hole punch with peer %s failed", rp)
}

func (hs *HolePunchService) handlerError(p peer.ID, err error) {
	hs.tracer.ProtocolError(p, err)
	log.Warn(err)
}

func (hs *HolePunchService) handleNewStream(s network.Stream) {
	log.Infof("got hole punch request from peer %s", s.Conn().RemotePeer().Pretty())
	_ = s.SetDeadline(time.Now().Add(StreamTimeout))
	rp := s.Conn().RemotePeer()
	wr := protoio.NewDelimitedWriter(s)
	rd := protoio.NewDelimitedReader(s, maxMsgSize)

	// Read Connect message
	msg := new(pb.HolePunch)
	if err := rd.ReadMsg(msg); err != nil {
		s.Reset()
		hs.handlerError(rp, fmt.Errorf("failed to read message from initator: %w", err))
		return
	}
	if t := msg.GetType(); t != pb.HolePunch_CONNECT {
		s.Reset()
		hs.handlerError(rp, fmt.Errorf("expected CONNECT message from initiator but got %d", t))
		return
	}
	obsDial := addrsFromBytes(msg.ObsAddrs)

	// Write CONNECT message
	msg.Reset()
	msg.Type = pb.HolePunch_CONNECT.Enum()
	msg.ObsAddrs = addrsToBytes(hs.ids.OwnObservedAddrs())
	tstart := time.Now()
	if err := wr.WriteMsg(msg); err != nil {
		s.Reset()
		hs.handlerError(rp, fmt.Errorf("failed to write CONNECT message to initator:: %w", err))
		return
	}

	// Read SYNC message
	msg.Reset()
	if err := rd.ReadMsg(msg); err != nil {
		s.Reset()
		hs.handlerError(rp, fmt.Errorf("failed to read message from initator: %w", err))
		return
	}
	rtt := time.Since(tstart)

	if t := msg.GetType(); t != pb.HolePunch_SYNC {
		s.Reset()
		hs.handlerError(rp, fmt.Errorf("expected SYNC message from initiator but got %d", t))
		return
	}
	defer s.Close()

	// Hole punch now by forcing a connect
	pi := peer.AddrInfo{
		ID:    rp,
		Addrs: obsDial,
	}
	hs.tracer.StartHolePunch(rp, obsDial, rtt)
	log.Debugf("starting hole punch with %s", rp)
	counter := atomic.AddInt32(&hs.counter, 1)
	tstart = time.Now()
	err := hs.holePunchConnect(pi, int(counter))
	dt := time.Since(tstart)
	hs.tracer.EndHolePunch(rp, dt, err)
	if err != nil {
		log.Warnf("hole punching with %s failed after %s: %s", rp, dt, err)
	} else {
		log.Infof("hole punching with %s successful after %s", rp, dt)
	}
}

func (hs *HolePunchService) holePunchConnect(pi peer.AddrInfo, attempt int) error {
	holePunchCtx := network.WithSimultaneousConnect(hs.ctx, "hole-punching")
	forceDirectConnCtx := network.WithForceDirectDial(holePunchCtx, "hole-punching")
	dialCtx, cancel := context.WithTimeout(forceDirectConnCtx, dialTimeout)
	defer cancel()

	hs.tracer.HolePunchAttempt(pi.ID, attempt)
	err := hs.host.Connect(dialCtx, pi)
	if err == nil {
		log.Infof("hole punch with peer %s successful after %d retries; direct conns to peer are:", pi.ID, attempt)
		for _, c := range hs.host.Network().ConnsToPeer(pi.ID) {
			if !isRelayAddress(c.RemoteMultiaddr()) {
				log.Info(c)
			}
		}
	}
	log.Infof("hole punch attempt %d with peer %s failed: %s; will retry in %s...", attempt, pi.ID, err, retryWait)
	return err
}

func isRelayAddress(a ma.Multiaddr) bool {
	_, err := a.ValueForProtocol(ma.P_CIRCUIT)
	return err == nil
}

func addrsToBytes(as []ma.Multiaddr) [][]byte {
	bzs := make([][]byte, 0, len(as))
	for _, a := range as {
		bzs = append(bzs, a.Bytes())
	}
	return bzs
}

func addrsFromBytes(bzs [][]byte) []ma.Multiaddr {
	addrs := make([]ma.Multiaddr, 0, len(bzs))
	for _, bz := range bzs {
		a, err := ma.NewMultiaddrBytes(bz)
		if err == nil {
			addrs = append(addrs, a)
		}
	}
	return addrs
}

type netNotifiee HolePunchService

func (nn *netNotifiee) HolePunchService() *HolePunchService {
	return (*HolePunchService)(nn)
}

func (nn *netNotifiee) Connected(_ network.Network, v network.Conn) {
	hs := nn.HolePunchService()
	dir := v.Stat().Direction

	// Hole punch if it's an inbound proxy connection.
	// If we already have a direct connection with the remote peer, this will be a no-op.
	if dir == network.DirInbound && isRelayAddress(v.RemoteMultiaddr()) {
		p := v.RemotePeer()
		hs.activeMx.Lock()
		_, active := hs.active[p]
		if !active {
			hs.active[p] = struct{}{}
		}
		hs.activeMx.Unlock()

		if active {
			return
		}

		log.Debugf("got inbound proxy conn from peer %s, connectionID is %s", v.RemotePeer().String(), v.ID())
		hs.refCount.Add(1)
		go func() {
			defer hs.refCount.Done()
			defer func() {
				hs.activeMx.Lock()
				delete(hs.active, p)
				hs.activeMx.Unlock()
			}()
			select {
			// waiting for Identify here will allow us to access the peer's public and observed addresses
			// that we can dial to for a hole punch.
			case <-hs.ids.IdentifyWait(v):
			case <-hs.ctx.Done():
				return
			}

			if err := hs.HolePunch(v.RemotePeer()); err != nil {
				log.Warnf("hole punching attempt with %s failed: %s", v.RemotePeer(), err)
			}
		}()
		return
	}
}

// NO-OPS
func (nn *netNotifiee) Disconnected(_ network.Network, v network.Conn)   {}
func (nn *netNotifiee) OpenedStream(n network.Network, v network.Stream) {}
func (nn *netNotifiee) ClosedStream(n network.Network, v network.Stream) {}
func (nn *netNotifiee) Listen(n network.Network, a ma.Multiaddr)         {}
func (nn *netNotifiee) ListenClose(n network.Network, a ma.Multiaddr)    {}
