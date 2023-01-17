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
	pool "github.com/libp2p/go-buffer-pool"
	"github.com/pion/ice/v2"
	"github.com/pion/stun"
)

var log = logging.Logger("mux")

const ReceiveMTU = 1500

var _ ice.UDPMux = &udpMux{}

type ufragConnKey struct {
	ufrag  string
	isIPv6 bool
}

// udpMux multiplexes multiple ICE connections over a single net.PacketConn,
// generally a UDP socket.
//
// The connections are indexed by (ufrag, IP address family)
// and by remote address from which the connection has received valid STUN/RTC
// packets.
//
// When a new packet is received on the underlying net.PacketConn, we
// first check the address map to see if there is a connection associated with the
// remote address. If found we forward the packet to the connection. If an associated
// connection is not found, we check to see if the packet is a STUN packet. We then
// fetch the ufrag of the remote from the STUN packet and use it to check if there
// is a connection associated with the (ufrag, IP address family) pair. If found
// we add the association to the address map. If not found, it is a previously
// unseen IP address and the `unknownUfragCallback` callback is invoked.
type udpMux struct {
	socket               net.PacketConn
	unknownUfragCallback func(string, net.Addr)

	storage *udpMuxStorage

	// the context controls the lifecycle of the mux
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

func NewUDPMux(socket net.PacketConn, unknownUfragCallback func(string, net.Addr)) ice.UDPMux {
	ctx, cancel := context.WithCancel(context.Background())
	mux := &udpMux{
		ctx:                  ctx,
		cancel:               cancel,
		socket:               socket,
		unknownUfragCallback: unknownUfragCallback,
		storage:              newUdpMuxStorage(),
	}

	mux.wg.Add(1)
	go mux.readLoop()
	return mux
}

// GetListenAddresses implements ice.UDPMux
func (mux *udpMux) GetListenAddresses() []net.Addr {
	return []net.Addr{mux.socket.LocalAddr()}
}

// GetConn implements ice.UDPMux
// It creates a net.PacketConn for a given ufrag if an existing
// one cannot be  found. We differentiate IPv4 and IPv6 addresses
// as a remote is capable of being reachable through multiple different
// UDP addresses of the same IP address family (eg. Server-reflexive addresses
// and peer-reflexive addresses).
func (mux *udpMux) GetConn(ufrag string, addr net.Addr) (net.PacketConn, error) {
	a, ok := addr.(*net.UDPAddr)
	isIPv6 := ok && a.IP.To4() == nil
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
	mux.socket.Close()
	mux.wg.Wait()
	return nil
}

// RemoveConnByUfrag implements ice.UDPMux
func (mux *udpMux) RemoveConnByUfrag(ufrag string) {
	mux.storage.RemoveConnByUfrag(ufrag)
}

func (mux *udpMux) getOrCreateConn(ufrag string, isIPv6 bool) (net.PacketConn, error) {
	select {
	case <-mux.ctx.Done():
		return nil, io.ErrClosedPipe
	default:
	}

	key := ufragConnKey{ufrag: ufrag, isIPv6: isIPv6}

	if conn, ok := mux.storage.GetConn(key); ok {
		return conn, nil
	}

	conn := newMuxedConnection(mux, ufrag)
	mux.storage.AddConn(key, conn)

	return conn, nil
}

// writeTo writes a packet to the underlying net.PacketConn
func (mux *udpMux) writeTo(buf []byte, addr net.Addr) (int, error) {
	return mux.socket.WriteTo(buf, addr)
}

func (mux *udpMux) readLoop() {
	defer mux.wg.Done()
	for {
		select {
		case <-mux.ctx.Done():
			return
		default:
		}

		buf := pool.Get(ReceiveMTU + 2048)

		n, addr, err := mux.socket.ReadFrom(buf)
		if err != nil {
			log.Errorf("error reading from socket: %v", err)
			pool.Put(buf)
			if os.IsTimeout(err) {
				continue
			}
			return
		}
		buf = buf[:n]

		// a non-nil error signifies that the packet was not
		// passed on to any connection, and therefore the current
		// function has ownership of the packet. Otherwise, the
		// ownership of the packet is passed to a connection
		processErr := mux.processPacket(buf, addr)
		if processErr != nil {
			buf = buf[:cap(buf)]
			pool.Put(buf)
		}
	}
}

func (mux *udpMux) processPacket(buf []byte, addr net.Addr) error {
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		return fmt.Errorf("underlying connection did not return a UDP address")
	}
	isIPv6 := udpAddr.IP.To4() == nil

	// Connections are indexed by remote address. We firest
	// check if the remote address has a connection associated
	// with it. If yes, we push the received packet to the connection
	// and loop again.
	conn, ok := mux.storage.GetConnByAddr(addr.String())
	// if address was not found check if ufrag exists
	if !ok && stun.IsMessage(buf) {
		msg := &stun.Message{Raw: buf}
		if err := msg.Decode(); err != nil || msg.Type != stun.BindingRequest {
			log.Debug("incoming message should be a STUN binding request")
			return err
		}

		ufrag, err := ufragFromStunMessage(msg)
		if err != nil {
			log.Debug("could not find STUN username: %w", err)
			return err
		}

		key := ufragConnKey{ufrag: ufrag, isIPv6: isIPv6}
		mux.storage.AddAddr(key, udpAddr.String(), func() *muxedConnection {
			return newMuxedConnection(mux, ufrag)
		})

		if !ok && mux.unknownUfragCallback != nil {
			mux.unknownUfragCallback(ufrag, udpAddr)
		}
	}

	if conn != nil {
		err := conn.push(buf, addr)
		if err != nil {
			log.Error("could not push packet")
		}
		return nil
	}

	return fmt.Errorf("connection not found")
}

// ufragFromStunMessage returns the local or ufrag
// from the STUN username attribute. Local ufrag is the ufrag of the
// peer which initiated the connectivity check, e.g in a connectivity
// check from A to B, the username attribute will be B_ufrag:A_ufrag
// with the local ufrag value being A_ufrag. In case of ice-lite, the
// localUfrag value will always be the remote peer's ufrag since ICE-lite
// implementations do not generate connectivity checks. In our specific
// case, since the local and remote ufrag is equal, we can return
// either value.
func ufragFromStunMessage(msg *stun.Message) (string, error) {
	attr, err := msg.Get(stun.AttrUsername)
	if err != nil {
		return "", err
	}
	ufrag := strings.Split(string(attr), ":")
	if len(ufrag) < 2 {
		return "", fmt.Errorf("invalid STUN username attribute")
	}
	return ufrag[1], nil
}

type udpMuxStorage struct {
	sync.RWMutex

	ufragMap map[ufragConnKey]*muxedConnection
	addrMap  map[string]*muxedConnection
}

func newUdpMuxStorage() *udpMuxStorage {
	return &udpMuxStorage{
		ufragMap: make(map[ufragConnKey]*muxedConnection),
		addrMap:  make(map[string]*muxedConnection),
	}
}

func (storage *udpMuxStorage) RemoveConnByUfrag(ufrag string) {
	if ufrag == "" {
		return
	}

	storage.Lock()
	defer storage.Unlock()

	for _, isIPv6 := range []bool{true, false} {
		key := ufragConnKey{ufrag: ufrag, isIPv6: isIPv6}
		if conn, ok := storage.ufragMap[key]; ok {
			_ = conn.closeConnection()
			delete(storage.ufragMap, key)
			for _, addr := range conn.addresses {
				delete(storage.addrMap, addr)
			}
		}
	}
}

func (storage *udpMuxStorage) GetConn(key ufragConnKey) (*muxedConnection, bool) {
	storage.RLock()
	conn, ok := storage.ufragMap[key]
	storage.RUnlock()
	return conn, ok
}

func (storage *udpMuxStorage) GetConnByAddr(addr string) (*muxedConnection, bool) {
	storage.RLock()
	conn, ok := storage.addrMap[addr]
	storage.RUnlock()
	return conn, ok
}

func (storage *udpMuxStorage) AddConn(key ufragConnKey, conn *muxedConnection) {
	storage.Lock()
	storage.ufragMap[key] = conn
	storage.Unlock()
}

func (storage *udpMuxStorage) AddAddr(key ufragConnKey, addr string, createConnIfNeeded func() *muxedConnection) {
	storage.Lock()
	defer storage.Unlock()

	conn, ok := storage.ufragMap[key]
	if !ok {
		conn = createConnIfNeeded()
		storage.ufragMap[key] = conn
	}

	storage.addrMap[addr] = conn
	conn.addresses = append(conn.addresses, addr)
}
