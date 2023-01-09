package libp2pwebrtc

import (
	"context"
	"fmt"
	"os"
	"sync"

	ic "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	tpt "github.com/libp2p/go-libp2p/core/transport"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/pion/webrtc/v3"
)

var _ tpt.CapableConn = &connection{}

const (
	maxAcceptQueueLen int = 10
)

type connection struct {
	pc        *webrtc.PeerConnection
	transport *WebRTCTransport
	scope     network.ConnManagementScope

	localPeer      peer.ID
	privKey        ic.PrivKey
	localMultiaddr ma.Multiaddr

	remotePeer      peer.ID
	remoteKey       ic.PubKey
	remoteMultiaddr ma.Multiaddr

	m       sync.Mutex
	streams map[uint16]*dataChannel

	acceptQueue chan network.MuxedStream

	ctx    context.Context
	cancel context.CancelFunc
}

func newConnection(
	pc *webrtc.PeerConnection,
	transport *WebRTCTransport,
	scope network.ConnManagementScope,

	localPeer peer.ID,
	privKey ic.PrivKey,
	localMultiaddr ma.Multiaddr,

	remotePeer peer.ID,
	remoteKey ic.PubKey,
	remoteMultiaddr ma.Multiaddr,
) *connection {
	acceptQueue := make(chan network.MuxedStream, maxAcceptQueueLen)

	ctx, cancel := context.WithCancel(context.Background())

	conn := &connection{
		pc:        pc,
		transport: transport,
		scope:     scope,

		localPeer:      localPeer,
		privKey:        privKey,
		localMultiaddr: localMultiaddr,

		remotePeer:      remotePeer,
		remoteKey:       remoteKey,
		remoteMultiaddr: remoteMultiaddr,
		ctx:             ctx,
		cancel:          cancel,
		streams:         make(map[uint16]*dataChannel),

		acceptQueue: acceptQueue,
	}

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		id := *dc.ID()
		var stream *dataChannel
		dc.OnOpen(func() {
			// datachannel cannot be detached before opening
			rwc, err := dc.Detach()
			if err != nil {
				log.Errorf("[%s] could not detach channel: %s", localPeer, dc.Label())
				return
			}
			stream = newDataChannel(conn, dc, rwc, pc, nil, nil)
			select {
			case acceptQueue <- stream:
				conn.addStream(id, stream)
			default:
				log.Warn("cannot buffer incoming stream, will reset")
				stream.Reset()
			}

		})

	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateDisconnected:
			log.Warn("peer connection disconnected")
		case webrtc.PeerConnectionStateFailed:
			log.Warn("peer connection reset")
			conn.resetStreams()
			fallthrough
		case webrtc.PeerConnectionStateClosed:
			log.Warn("peer connection closed")
			conn.Close()
		}
	})

	return conn
}

func (c *connection) resetStreams() {
	if c.IsClosed() {
		return
	}
	for _, stream := range c.streams {
		stream.Reset()
	}
}

// ConnState implements transport.CapableConn
func (c *connection) ConnState() network.ConnectionState {
	return network.ConnectionState{
		Transport: "webrtc",
	}
}

// Implement network.MuxedConn

func (c *connection) Close() error {
	if c.IsClosed() {
		return nil
	}

	c.scope.Done()
	c.cancel()
	return c.pc.Close()
}

func (c *connection) IsClosed() bool {
	select {
	case <-c.ctx.Done():
		return true
	default:
	}
	return false
}

func (c *connection) OpenStream(ctx context.Context) (network.MuxedStream, error) {
	type openStreamResult struct {
		network.MuxedStream
		error
	}

	if c.IsClosed() {
		return nil, os.ErrClosed
	}

	result := make(chan openStreamResult)
	dc, err := c.pc.CreateDataChannel("", nil)
	if err != nil {
		return nil, err
	}

	// OnOpen will return immediately for detached datachannels
	// refer: https://github.com/pion/webrtc/blob/7ab3174640b3ce15abebc2516a2ca3939b5f105f/datachannel.go#L278-L282
	streamID := *dc.ID()
	var stream *dataChannel
	dc.OnOpen(func() {
		rwc, err := dc.Detach()
		if err != nil {
			select {
			case result <- openStreamResult{
				nil,
				fmt.Errorf("could not detach dataChannel: %w", err),
			}:
			default:
			}
			return
		}
		stream = newDataChannel(c, dc, rwc, c.pc, nil, nil)
		select {
		case result <- openStreamResult{stream, err}:
			c.addStream(streamID, stream)
		default:
			stream.Reset()
		}
	})

	dc.OnError(func(err error) {
		log.Warn("datachannel error: %w", err)
	})

	select {
	case <-ctx.Done():
		_ = dc.Close()
		return nil, ctx.Err()
	case r := <-result:
		return r.MuxedStream, r.error
	}
}

func (c *connection) AcceptStream() (network.MuxedStream, error) {
	select {
	case <-c.ctx.Done():
		return nil, os.ErrClosed
	case stream := <-c.acceptQueue:
		return stream, nil
	}
}

// implement network.ConnSecurity
func (c *connection) LocalPeer() peer.ID {
	return c.localPeer
}

// only used during setup
func (c *connection) setRemotePeer(id peer.ID) {
	c.remotePeer = id
}

func (c *connection) setRemotePublicKey(key ic.PubKey) {
	c.remoteKey = key
}

func (c *connection) LocalPrivateKey() ic.PrivKey {
	return c.privKey
}

func (c *connection) RemotePeer() peer.ID {
	return c.remotePeer
}

func (c *connection) RemotePublicKey() ic.PubKey {
	return c.remoteKey
}

// implement network.ConnMultiaddrs
func (c *connection) LocalMultiaddr() ma.Multiaddr {
	return c.localMultiaddr
}

func (c *connection) RemoteMultiaddr() ma.Multiaddr {
	return c.remoteMultiaddr
}

// implement network.ConnScoper
func (c *connection) Scope() network.ConnScope {
	return c.scope
}

func (c *connection) Transport() tpt.Transport {
	return c.transport
}

func (c *connection) addStream(id uint16, stream *dataChannel) {
	c.m.Lock()
	defer c.m.Unlock()
	c.streams[id] = stream
}

func (c *connection) removeStream(id uint16) {
	c.m.Lock()
	defer c.m.Unlock()
	delete(c.streams, id)
}
