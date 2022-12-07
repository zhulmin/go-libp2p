package udpmux

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/pion/ice/v2"
	"github.com/pion/stun"
)

const receiveMTU = 1500

var _ ice.UDPMux = &udpMux{}

type ufragConnKey struct {
	ufrag  string
	isIPv6 bool
}

// udpMux multiplexes multiple ICE connections over a single net.PacketConn,
// generally a UDP socket. The connections are indexed by (ufrag, IP address type)
// and by remote address from which the connection has received valid STUN/RTC
// packets.
type udpMux struct {
	mu                   sync.Mutex
	wg                   sync.WaitGroup
	ctx                  context.Context
	cancel               context.CancelFunc
	socket               net.PacketConn
	unknownUfragCallback func(string, net.Addr)
	ufragMap             map[ufragConnKey]*muxedConnection
	addrMap              map[string]*muxedConnection
}

func NewUDPMux(socket net.PacketConn, unknownUfragCallback func(string, net.Addr)) ice.UDPMux {
	ctx, cancel := context.WithCancel(context.Background())
	mux := &udpMux{
		ctx:                  ctx,
		cancel:               cancel,
		socket:               socket,
		unknownUfragCallback: unknownUfragCallback,
		ufragMap:             make(map[ufragConnKey]*muxedConnection),
		addrMap:              make(map[string]*muxedConnection),
	}

	mux.wg.Add(1)
	go mux.readLoop()
	return mux
}

// GetConn implements ice.UDPMux
// It creates a net.PacketConn for a given ufrag if an existing
// one cannot be  found.
func (mux *udpMux) GetConn(ufrag string, isIPv6 bool) (net.PacketConn, error) {
	return mux.getOrCreateConn(ufrag, isIPv6)
}

// Close implements ice.UDPMux
func (mux *udpMux) Close() error {
	select {
	case <-mux.ctx.Done():
		return nil
	default:
	}
	mux.cancel()
	mux.wg.Wait()
	return nil
}

// RemoveConnByUfrag implements ice.UDPMux
func (mux *udpMux) RemoveConnByUfrag(ufrag string) {
	mux.mu.Lock()
	defer mux.mu.Unlock()
	for _, isIPv6 := range []bool{true, false} {
		key := ufragConnKey{ufrag: ufrag, isIPv6: isIPv6}
		if conn, ok := mux.ufragMap[key]; ok {
			_ = conn.closeConnection()
			delete(mux.ufragMap, key)
			for _, addr := range conn.addresses {
				delete(mux.addrMap, addr)
			}
		}
	}
}

func (mux *udpMux) getOrCreateConn(ufrag string, isIPv6 bool) (net.PacketConn, error) {
	select {
	case <-mux.ctx.Done():
		return nil, io.ErrClosedPipe
	default:
	}
	key := ufragConnKey{ufrag: ufrag, isIPv6: isIPv6}
	mux.mu.Lock()
	defer mux.mu.Unlock()
	// check if the required connection exists
	if conn, ok := mux.ufragMap[key]; ok {
		return conn, nil
	}

	conn := newMuxedConnection(mux, ufrag)
	mux.ufragMap[key] = conn

	return conn, nil
}

// writeTo writes a packet to the underlying net.PacketConn
func (mux *udpMux) writeTo(buf []byte, addr net.Addr) (int, error) {
	return mux.socket.WriteTo(buf, addr)
}

func (mux *udpMux) readLoop() {
	buf := make([]byte, receiveMTU)
	for {
		select {
		case <-mux.ctx.Done():
			return
		default:
		}
		buf = buf[:cap(buf)]
		n, addr, err := mux.socket.ReadFrom(buf)
		buf = buf[:n]
		if err != nil {
			if os.IsTimeout(err) {
				continue
			}
			return
		}
		udpAddr := addr.(*net.UDPAddr)
		isIPv6 := udpAddr.IP.To4() == nil

		// Connections are indexed by remote address. We firest
		// check if the remote address has a connection associated
		// with it. If yes, we push the received packet to the connection
		// and loop again.
		mux.mu.Lock()
		conn, ok := mux.addrMap[udpAddr.String()]
		mux.mu.Unlock()
		// if address was not found check if ufrag exists
		if !ok && stun.IsMessage(buf) {
			msg := &stun.Message{Raw: buf}
			if err := msg.Decode(); err != nil {
				continue
			}
			ufrag, err := ufragFromStunMessage(msg, false)
			if err != nil {
				continue
			}
			key := ufragConnKey{ufrag: ufrag, isIPv6: isIPv6}
			mux.mu.Lock()
			conn, ok = mux.ufragMap[key]
			if !ok {
				conn = newMuxedConnection(mux, ufrag)
				mux.ufragMap[key] = conn
				if mux.unknownUfragCallback != nil {
					mux.unknownUfragCallback(ufrag, udpAddr)
				}
			}
			conn.addresses = append(conn.addresses, udpAddr.String())
			mux.addrMap[udpAddr.String()] = conn
			mux.mu.Unlock()
		}

		if conn != nil {
			_ = conn.push(buf[:n], udpAddr)
		}
	}
}

// ufragFromStunMessage returns either the local or remote ufrag
// from the STUN username attribute. Local ufrag is the ufrag of the
// peer which initiated the connectivity check, e.g in a connectivity
// check from A to B, the username attribute will be B_ufrag:A_ufrag
// with the local ufrag value being A_ufrag. In case of ice-lite, the
// localUfrag value will always be the remote peer's ufrag since ICE-lite
// implementations do not generate connectivity checks.
func ufragFromStunMessage(msg *stun.Message, localUfrag bool) (string, error) {
	attr, err := msg.Get(stun.AttrUsername)
	if err != nil {
		return "", err
	}
	ufrag := strings.Split(string(attr), ":")
	if len(ufrag) < 2 {
		return "", fmt.Errorf("invalid STUN username attribute")
	}
	if localUfrag {
		return ufrag[1], nil
	}
	return ufrag[0], nil
}
