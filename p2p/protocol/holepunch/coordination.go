package holepunch

import (
	"context"
	"fmt"
	"sync"
	"time"

	logging "github.com/ipfs/go-log"
	"github.com/jpillora/backoff"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	holepunch_pb "github.com/libp2p/go-libp2p/p2p/protocol/holepunch/pb"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
	"github.com/libp2p/go-msgio/protoio"
	ma "github.com/multiformats/go-multiaddr"
)

// TODO Should we have options for these ?
const (
	protocol             = "/libp2p/holepunch/1.0.0"
	maxMsgSize           = 1 * 1024 // 1K
	holePunchTimeout     = 2 * time.Minute
	dialTimeout          = 60 * time.Second
	connCloseGracePeriod = 5 * time.Minute
	maxRetries           = 4
)

var (
	log = logging.Logger("p2p/holepunch")
)

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

	h.SetStreamHandler(protocol, hs.handleNewStream)
	h.Network().Notify((*netNotifiee)(hs))
	return hs, nil
}

// Close closes the Hole Punch Service.
func (hs *HolePunchService) Close() error {
	hs.closeSync.Do(func() {
		hs.ctxCancel()
		hs.refCount.Wait()
	})

	return nil
}

// attempts to make a direct connection with the remote peer of `relayConn`
// by co-ordinating a hole punch over the given relay connection `relayConn`.
func (hs *HolePunchService) holePunch(relayConn network.Conn) {
	fmt.Println("\n hole punching for connection", relayConn)
	rp := relayConn.RemotePeer()

	// attempt a direct connection
	forceDirectConnCtx := network.WithForceDirectDial(hs.ctx, "hole-punching")
	dialCtx, cancel := context.WithTimeout(forceDirectConnCtx, dialTimeout)
	defer cancel()
	if err := hs.host.Connect(dialCtx, peer.AddrInfo{ID: rp}); err == nil {
		fmt.Printf("\n direct connection to peer %s successful, no need for a hole punch", rp.Pretty())
		return
	}

	// hole punch
	s, err := hs.host.NewStream(hs.ctx, rp, protocol)
	if err != nil {
		fmt.Printf("\n failed to open hole punching stream: %s", err)
		return
	}
	_ = s.SetDeadline(time.Now().Add(holePunchTimeout))
	w := protoio.NewDelimitedWriter(s)

	// send a PING and start RTT measurement
	msg := new(holepunch_pb.HolePunch)
	msg.Type = holepunch_pb.HolePunch_PING.Enum()
	if err := w.WriteMsg(msg); err != nil {
		s.Reset()
		fmt.Printf("\n failed to send hole punch PING: %s", err)
		return
	}
	tstart := time.Now()

	// wait for a pong
	rd := protoio.NewDelimitedReader(s, maxMsgSize)
	msg.Reset()
	if err := rd.ReadMsg(msg); err != nil {
		s.Reset()
		fmt.Printf("\n failed to read hole punch PONG: %s", err)
		return
	}
	if msg.GetType() != holepunch_pb.HolePunch_PONG {
		s.Reset()
		fmt.Printf("\n did not get expected pong message")
		return
	}
	rtt := time.Since(tstart)
	fmt.Printf("\n RTT is %d milliseconds", rtt.Milliseconds())

	// send a SYNC message and attempt a direct connect after half the RTT
	msg.Reset()
	msg.Type = holepunch_pb.HolePunch_SYNC.Enum()
	if err := w.WriteMsg(msg); err != nil {
		s.Reset()
		fmt.Printf("\n failed to send SYNC message for Hole punching: %s", err)
		return
	}
	defer s.Close()

	synTime := time.Duration(rtt.Milliseconds()/2) * time.Millisecond
	fmt.Printf("\n sync time is %d milliseconds", synTime.Milliseconds())

	// wait for sync to reach the other peer and then punch a hole for it in our NAT
	// by attempting a connect to it.
	select {
	case <-time.After(synTime):
		dialCtx, cancel := context.WithTimeout(forceDirectConnCtx, dialTimeout)
		defer cancel()
		err := hs.host.Connect(dialCtx, peer.AddrInfo{ID: rp})
		if err == nil {
			fmt.Println("\n hole punch successful !!!!")
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
			err = hs.host.Connect(dialCtx, peer.AddrInfo{ID: rp})
			if err == nil {
				fmt.Println("\n hole punch successful !")
				return
			}
		}
		fmt.Printf("\n hole punch failed: %s", err)

	case <-hs.ctx.Done():
		fmt.Println("\n hole punch ctx cancelled")
		return
	}
}

func (hs *HolePunchService) handleNewStream(s network.Stream) {
	_ = s.SetDeadline(time.Now().Add(holePunchTimeout))
	rp := s.Conn().RemotePeer()
	wr := protoio.NewDelimitedWriter(s)

	rd := protoio.NewDelimitedReader(s, maxMsgSize)
	// Read Ping message
	msg := new(holepunch_pb.HolePunch)
	if err := rd.ReadMsg(msg); err != nil {
		s.Reset()
		return
	}
	if msg.GetType() != holepunch_pb.HolePunch_PING {
		s.Reset()
		return
	}

	// Write PONG message
	msg.Reset()
	msg.Type = holepunch_pb.HolePunch_PONG.Enum()
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
	if msg.GetType() != holepunch_pb.HolePunch_SYNC {
		s.Reset()
		return
	}
	defer s.Close()

	// Hole punch now by forcing a connect
	forceDirectConnCtx := network.WithForceDirectDial(hs.ctx, "hole-punching")
	dialCtx, cancel := context.WithTimeout(forceDirectConnCtx, dialTimeout)
	defer cancel()
	fmt.Printf("\n response peer %s will hole punch now in response to sync message", hs.host.ID().Pretty())
	err := hs.host.Connect(dialCtx, peer.AddrInfo{ID: rp})
	if err == nil {
		fmt.Println("\n Hole punch successful")
		return
	}

	// First hole punch attempt failed, implement a backoff-retry now
	// before giving up.
	b := &backoff.Backoff{
		Jitter: true,
		Min:    1 * time.Second,
		Max:    5 * time.Second,
		Factor: 2,
	}
	for b.Attempt() < maxRetries {
		time.Sleep(b.Duration())
		err = hs.host.Connect(dialCtx, peer.AddrInfo{ID: rp})
		if err == nil {
			fmt.Println("\n hole punch successful !")
			return
		}
	}

	fmt.Printf("\n hole punch failed: %s", err)
}

type netNotifiee HolePunchService

func (nn *netNotifiee) HolePunchService() *HolePunchService {
	return (*HolePunchService)(nn)
}

func (nn *netNotifiee) Connected(_ network.Network, v network.Conn) {
	hs := nn.HolePunchService()
	rp := v.RemotePeer()
	dir := v.Stat().Direction

	// Hole punch if it's an inbound proxy connection.
	if dir == network.DirInbound && !isDirectConn(v) {
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

	// If we see a direct connection when we already have a Proxy connection, process it
	// as a hole punched connection and scheduled the Proxy connection to be closed after a grace period.
	if isDirectConn(v) {
		connsToPeer := hs.host.Network().ConnsToPeer(rp)
		for _, c := range connsToPeer {
			// proxy connection -> schedule it to be closed after a grace period.
			if !isDirectConn(c) {
				time.AfterFunc(connCloseGracePeriod, func() {
					// do we have direct connectivity with the peer ?
					isDirect := false
					for _, c := range hs.host.Network().ConnsToPeer(rp) {
						if isDirectConn(c) {
							isDirect = true
						}
					}

					if isDirect {
						// TODO If we have two direct connections, we should keep only one around ?
						// Especially for QUIC as QUIC hole punching can result into two direct connections.
						c.Close()
					}
				})
			}
		}
	}
}

func (nn *netNotifiee) Disconnected(_ network.Network, v network.Conn) {}

func (nn *netNotifiee) OpenedStream(n network.Network, v network.Stream) {}
func (nn *netNotifiee) ClosedStream(n network.Network, v network.Stream) {}
func (nn *netNotifiee) Listen(n network.Network, a ma.Multiaddr)         {}
func (nn *netNotifiee) ListenClose(n network.Network, a ma.Multiaddr)    {}

func isDirectConn(c network.Conn) bool {
	_, err := c.RemoteMultiaddr().ValueForProtocol(ma.P_CIRCUIT)

	return err != nil
}
