package udpmux

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"

	logging "github.com/ipfs/go-log/v2"
	"github.com/pion/ice/v2"
	"github.com/pion/stun"
)

const receiveMTU = 1500

var _ ice.UDPMux = &udpMux{}

var log = logging.Logger("webrtc-transport-mux")

type ufragConnKey struct {
	ufrag  string
	isIPv6 bool
}

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
	log.Warn("waiting for close")
	mux.wg.Wait()
	return nil
}

// RemoveConnByUfrag implements ice.UDPMux
func (mux *udpMux) RemoveConnByUfrag(ufrag string) {
	mux.mu.Lock()
	defer mux.mu.Unlock()
	removedAddresses := []string{}
	for _, isIPv6 := range []bool{true, false} {
		key := ufragConnKey{ufrag: ufrag, isIPv6: isIPv6}
		if conn, ok := mux.ufragMap[key]; ok {
			_ = conn.closeConnection()
			removedAddresses = append(removedAddresses, conn.addresses...)
			delete(mux.ufragMap, key)
		}
	}

	for _, addr := range removedAddresses {
		delete(mux.addrMap, addr)
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
	// check if the required connection exists
	if conn, ok := mux.ufragMap[key]; ok {
		mux.mu.Unlock()
		return conn, nil
	}

	conn := newMuxedConnection(mux, ufrag)
	mux.ufragMap[key] = conn
	mux.mu.Unlock()
	return conn, nil
}

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
				// the flaky bit of code
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

func (mux *udpMux) hasConn(ufrag string) net.PacketConn {
	mux.mu.Lock()
	defer mux.mu.Unlock()

	for _, isIPv6 := range []bool{true, false} {
		key := ufragConnKey{ufrag: ufrag, isIPv6: isIPv6}
		if conn, ok := mux.ufragMap[key]; ok {
			return conn
		}
	}
	return nil
}

func ufragFromStunMessage(msg *stun.Message, local_ufrag bool) (string, error) {
	attr, stunAttrErr := msg.Get(stun.AttrUsername)
	if stunAttrErr != nil {
		return "", stunAttrErr
	}
	ufrag := strings.Split(string(attr), ":")
	if len(ufrag) < 2 {
		return "", fmt.Errorf("invalid STUN username attribute")
	}
	if local_ufrag {
		return ufrag[1], nil
	} else {
		return ufrag[0], nil
	}
}
