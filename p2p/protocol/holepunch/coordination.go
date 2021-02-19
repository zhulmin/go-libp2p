package holepunch

import (
	"context"
	"sync"
	"time"

	logging "github.com/ipfs/go-log"
	"github.com/jpillora/backoff"
	"github.com/libp2p/go-libp2p-core/event"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	pb "github.com/libp2p/go-libp2p/p2p/protocol/holepunch/pb"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/libp2p/go-msgio/protoio"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// TODO Should we have options for these ?
const (
	protocol         = "/libp2p/holepunch/1.0.0"
	maxMsgSize       = 4 * 1024 // 4K
	holePunchTimeout = 1 * time.Minute
	dialTimeout      = 10 * time.Second
	maxRetries       = 4
)

var (
	log = logging.Logger("p2p-holepunch")
)

// TODO Find a better name for this protocol.
// HolePunchService is used to make direct connections with a peer via hole-punching.
type HolePunchService struct {
	ctx       context.Context
	ctxCancel context.CancelFunc

	ids  *identify.IDService
	host host.Host

	// ensure we shutdown ONLY once
	closeSync sync.Once
	refCount  sync.WaitGroup
}

// NewHolePunchService creates a new service that can be used for hole punching
func NewHolePunchService(h host.Host, ids *identify.IDService) (*HolePunchService, error) {
	ctx, cancel := context.WithCancel(context.Background())
	hs := &HolePunchService{ctx: ctx, ctxCancel: cancel, host: h, ids: ids}

	sub, err := h.EventBus().Subscribe(new(event.EvtNATDeviceTypeChanged))
	if err != nil {
		return nil, err
	}

	h.SetStreamHandler(protocol, hs.handleNewStream)
	h.Network().Notify((*netNotifiee)(hs))

	hs.refCount.Add(1)
	go hs.loop(sub)

	return hs, nil
}

func (hs *HolePunchService) loop(sub event.Subscription) {
	defer hs.refCount.Done()
	defer sub.Close()

	for {
		select {
		// Our local NAT device types are intialized in the peerstore when the Host is created
		//	and updated in the peerstore by the Observed Address Manager.
		case _, ok := <-sub.Out():
			if !ok {
				return
			}

			if hs.peerSupportsHolePunching(hs.host.ID(), hs.host.Addrs()) {
				hs.host.SetStreamHandler(protocol, hs.handleNewStream)
			} else {
				hs.host.RemoveStreamHandler(protocol)
			}

		case <-hs.ctx.Done():
			return
		}
	}
}

func hasProtoAddr(protocCode int, addrs []ma.Multiaddr) bool {
	for _, a := range addrs {
		if _, err := a.ValueForProtocol(protocCode); err == nil {
			return true
		}
	}

	return false
}

// Close closes the Hole Punch Service.
func (hs *HolePunchService) Close() error {
	hs.closeSync.Do(func() {
		hs.ctxCancel()
		hs.refCount.Wait()
	})

	return nil
}

// attempts to make a direct connection with the remote peer of `relayConn` by co-ordinating a hole punch over
// the given relay connection `relayConn`.
func (hs *HolePunchService) holePunch(relayConn network.Conn) {
	rp := relayConn.RemotePeer()

	// short-circuit hole punching if a direct dial works.
	// attempt a direct connection ONLY if we have a public address for the remote peer
	for _, a := range hs.host.Peerstore().Addrs(rp) {
		if manet.IsPublicAddr(a) && !isRelayAddress(a) {
			forceDirectConnCtx := network.WithForceDirectDial(hs.ctx, "hole-punching")
			dialCtx, cancel := context.WithTimeout(forceDirectConnCtx, dialTimeout)
			defer cancel()
			if err := hs.host.Connect(dialCtx, peer.AddrInfo{ID: rp}); err == nil {
				log.Debugf("direct connection to peer %s successful, no need for a hole punch", rp.Pretty())
				return
			}
			break
		}
	}

	// return if either peer does NOT support hole punching
	if !hs.peerSupportsHolePunching(rp, hs.host.Peerstore().Addrs(rp)) ||
		!hs.peerSupportsHolePunching(hs.host.ID(), hs.host.Addrs()) {
		return
	}

	// hole punch
	hpCtx := network.WithUseTransient(hs.ctx, "hole-punch")
	sCtx := network.WithNoDial(hpCtx, "hole-punch")
	s, err := hs.host.NewStream(sCtx, rp, protocol)
	if err != nil {
		log.Errorf("failed to open hole-punching stream with peer %s, err: %s", rp, err)
		return
	}
	log.Infof("will attempt hole punch with peer %s", rp.Pretty())
	_ = s.SetDeadline(time.Now().Add(holePunchTimeout))
	w := protoio.NewDelimitedWriter(s)

	// send a CONNECT and start RTT measurement
	msg := new(pb.HolePunch)
	msg.Type = pb.HolePunch_CONNECT.Enum()
	msg.ObsAddrs = addrsToBytes(hs.ids.OwnObservedAddrs())
	if err := w.WriteMsg(msg); err != nil {
		s.Reset()
		log.Errorf("failed to send hole punch CONNECT, err: %s", err)
		return
	}
	tstart := time.Now()

	// wait for a CONNECT message from the remote peer
	rd := protoio.NewDelimitedReader(s, maxMsgSize)
	msg.Reset()
	if err := rd.ReadMsg(msg); err != nil {
		s.Reset()
		log.Errorf("failed to read connect message from remote peer, err: %s", err)
		return
	}
	if msg.GetType() != pb.HolePunch_CONNECT {
		s.Reset()
		log.Debugf("expectd HolePunch_CONNECT message, got %s", msg.GetType())
		return
	}
	obsRemote := addrsFromBytes(msg.ObsAddrs)
	rtt := time.Since(tstart)

	// send a SYNC message and attempt a direct connect after half the RTT
	msg.Reset()
	msg.Type = pb.HolePunch_SYNC.Enum()
	if err := w.WriteMsg(msg); err != nil {
		s.Reset()
		log.Errorf("failed to send SYNC message for hole punching, err: %s", err)
		return
	}
	defer s.Close()

	synTime := time.Duration(rtt.Milliseconds()/2) * time.Millisecond

	// wait for sync to reach the other peer and then punch a hole for it in our NAT
	// by attempting a connect to it.
	select {
	case <-time.After(synTime):
		pi := peer.AddrInfo{
			ID:    rp,
			Addrs: obsRemote,
		}
		hs.holePunchConnectWithBackoff(pi)

	case <-hs.ctx.Done():
		return
	}
}

// We can hole punch with a peer ONLY if it is NOT behind a symmetric NAT for all the transport protocol it supports.
func (hs *HolePunchService) peerSupportsHolePunching(p peer.ID, addrs []ma.Multiaddr) bool {
	udpSupported := hasProtoAddr(ma.P_UDP, addrs)
	tcpSupported := hasProtoAddr(ma.P_TCP, addrs)
	udpNAT, _ := hs.host.Peerstore().Get(p, identify.UDPNATDeviceTypeKey)
	tcpNAT, _ := hs.host.Peerstore().Get(p, identify.TCPNATDeviceTypeKey)
	udpNatType := udpNAT.(network.NATDeviceType)
	tcpNATType := tcpNAT.(network.NATDeviceType)

	if udpSupported {
		if udpNatType == network.NATDeviceTypeCone || udpNatType == network.NATDeviceTypeUnknown {
			return true
		}
	}

	if tcpSupported {
		if tcpNATType == network.NATDeviceTypeCone || tcpNATType == network.NATDeviceTypeUnknown {
			return true
		}
	}

	return false
}

func (hs *HolePunchService) handleNewStream(s network.Stream) {
	log.Infof("got hole punch request from peer %s", s.Conn().RemotePeer().Pretty())
	_ = s.SetDeadline(time.Now().Add(holePunchTimeout))
	rp := s.Conn().RemotePeer()
	wr := protoio.NewDelimitedWriter(s)
	rd := protoio.NewDelimitedReader(s, maxMsgSize)

	// Read Connect message
	msg := new(pb.HolePunch)
	if err := rd.ReadMsg(msg); err != nil {
		s.Reset()
		return
	}
	if msg.GetType() != pb.HolePunch_CONNECT {
		s.Reset()
		return
	}
	obsDial := addrsFromBytes(msg.ObsAddrs)

	// Write CONNECT message
	msg.Reset()
	msg.Type = pb.HolePunch_CONNECT.Enum()
	msg.ObsAddrs = addrsToBytes(hs.ids.OwnObservedAddrs())
	if err := wr.WriteMsg(msg); err != nil {
		s.Reset()
		return
	}

	// Read SYNC message
	msg.Reset()
	if err := rd.ReadMsg(msg); err != nil {
		s.Reset()
		return
	}
	if msg.GetType() != pb.HolePunch_SYNC {
		s.Reset()
		return
	}
	defer s.Close()

	// Hole punch now by forcing a connect
	pi := peer.AddrInfo{
		ID:    rp,
		Addrs: obsDial,
	}
	hs.holePunchConnectWithBackoff(pi)
}

func (hs *HolePunchService) holePunchConnectWithBackoff(pi peer.AddrInfo) {
	forceDirectConnCtx := network.WithForceDirectDial(hs.ctx, "hole-punching")
	dialCtx, cancel := context.WithTimeout(forceDirectConnCtx, dialTimeout)
	defer cancel()
	err := hs.host.Connect(dialCtx, pi)
	if err == nil {
		log.Infof("hole punch with peer %s successful, direct conns to peer are:", pi.ID.Pretty())
		for _, c := range hs.host.Network().ConnsToPeer(pi.ID) {
			if !isRelayAddress(c.RemoteMultiaddr()) {
				log.Info(c)
			}
		}
		return
	}

	// backoff and retry before giving up.
	// this code will make the peer retry for (approximately ) a TOTAL of 10 seconds
	// before giving up and declaring the hole punch a failure.
	b := &backoff.Backoff{
		Jitter: true,
		Min:    1 * time.Second,
		Max:    5 * time.Second,
		Factor: 2,
	}
	for b.Attempt() < maxRetries {
		time.Sleep(b.Duration())

		dialCtx, cancel := context.WithTimeout(forceDirectConnCtx, dialTimeout)
		defer cancel()

		err = hs.host.Connect(dialCtx, pi)
		if err == nil {
			log.Infof("hole punch with peer %s successful after retry, direct conns to peer are:", pi.ID.Pretty())
			for _, c := range hs.host.Network().ConnsToPeer(pi.ID) {
				if !isRelayAddress(c.RemoteMultiaddr()) {
					log.Info(c)
				}
			}
			return
		}
	}
	log.Errorf("hole punch with peer %s failed, err: %s", pi.ID.Pretty(), err)
}

type netNotifiee HolePunchService

func (nn *netNotifiee) HolePunchService() *HolePunchService {
	return (*HolePunchService)(nn)
}

// TODO FIX For some reason, we see two such notifications for inbound proxy connections.
func (nn *netNotifiee) Connected(_ network.Network, v network.Conn) {
	hs := nn.HolePunchService()
	dir := v.Stat().Direction

	// Hole punch if it's an inbound proxy connection.
	// If we already have a direct connection with the remote peer, this will be a no-op.
	if dir == network.DirInbound && isRelayAddress(v.RemoteMultiaddr()) {
		log.Debugf("got inbound proxy conn from peer %s, connection is %v", v.RemotePeer().String(), v)
		hs.refCount.Add(1)
		go func() {
			defer hs.refCount.Done()
			select {
			// waiting for Identify here will allow us to access the peer's public and observed addresses
			// that we can dial to for a hole punch.
			case <-hs.ids.IdentifyWait(v):
			case <-hs.ctx.Done():
				return
			}
			nn.HolePunchService().holePunch(v)
		}()
		return
	}
}

func (nn *netNotifiee) Disconnected(_ network.Network, v network.Conn) {}

func (nn *netNotifiee) OpenedStream(n network.Network, v network.Stream) {}
func (nn *netNotifiee) ClosedStream(n network.Network, v network.Stream) {}
func (nn *netNotifiee) Listen(n network.Network, a ma.Multiaddr)         {}
func (nn *netNotifiee) ListenClose(n network.Network, a ma.Multiaddr)    {}

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
