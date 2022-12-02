package libp2pwebrtc

import (
	"context"
	"crypto"
	"encoding/hex"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/p2p/transport/webrtc/udpmux"

	tpt "github.com/libp2p/go-libp2p/core/transport"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/multiformats/go-multibase"
	"github.com/multiformats/go-multihash"

	"github.com/pion/ice/v2"
	"github.com/pion/webrtc/v3"
)

var _ tpt.Listener = &listener{}

const (
	maxConnectionBufferSize int = 10
	candidateSetupTimeout       = 20 * time.Second
)

type candidateAddr struct {
	ufrag string
	raddr *net.UDPAddr
}

type listener struct {
	transport                 *WebRTCTransport
	config                    webrtc.Configuration
	localFingerprint          webrtc.DTLSFingerprint
	localFingerprintMultibase string
	mux                       ice.UDPMux
	ctx                       context.Context
	cancel                    context.CancelFunc
	localAddr                 net.Addr
	localMultiaddr            ma.Multiaddr
	connChan                  chan tpt.CapableConn
	wg                        sync.WaitGroup
}

func newListener(transport *WebRTCTransport, laddr ma.Multiaddr, socket net.PacketConn, config webrtc.Configuration) (*listener, error) {
	candidateChan := make(chan candidateAddr, 1)
	mux := udpmux.NewUDPMux(socket, func(ufrag string, addr net.Addr) {
		candidateChan <- candidateAddr{ufrag: ufrag, raddr: addr.(*net.UDPAddr)}
	})
	localFingerprints, err := config.Certificates[0].GetFingerprints()
	if err != nil {
		return nil, err
	}

	localMh, err := hex.DecodeString(strings.ReplaceAll(localFingerprints[0].Value, ":", ""))
	if err != nil {
		return nil, err
	}
	localMhBuf, _ := multihash.Encode(localMh, multihash.SHA2_256)
	localFpMultibase, _ := multibase.Encode(multibase.Base58BTC, localMhBuf)

	ctx, cancel := context.WithCancel(context.Background())

	l := &listener{
		mux:                       mux,
		transport:                 transport,
		config:                    config,
		localFingerprint:          localFingerprints[0],
		localFingerprintMultibase: localFpMultibase,
		localMultiaddr:            laddr,
		ctx:                       ctx,
		cancel:                    cancel,
		localAddr:                 socket.LocalAddr(),
		connChan:                  make(chan tpt.CapableConn, maxConnectionBufferSize),
	}

	l.wg.Add(1)
	go l.handleIncomingCandidates(candidateChan)
	return l, err
}

func (l *listener) handleIncomingCandidates(candidateChan chan candidateAddr) {
	defer l.wg.Done()
	for {
		select {
		case <-l.ctx.Done():
			return
		case addr := <-candidateChan:
			go func() {
				ctx, cancelFunc := context.WithTimeout(context.Background(), candidateSetupTimeout)
				defer cancelFunc()
				conn, err := l.handleCandidate(ctx, addr)
				if err != nil {
					log.Debugf("could not accept connection: %v", err)
					return
				}
				l.connChan <- conn
			}()
		}
	}
}

func (l *listener) Accept() (tpt.CapableConn, error) {
	select {
	case <-l.ctx.Done():
		return nil, os.ErrClosed
	case conn := <-l.connChan:
		return conn, nil
	}
}

func (l *listener) Close() error {
	select {
	case <-l.ctx.Done():
		return nil
	default:
	}
	l.cancel()
	l.wg.Wait()
	return nil
}

func (l *listener) Addr() net.Addr {
	return l.localAddr
}

func (l *listener) Multiaddrs() []ma.Multiaddr {
	return []ma.Multiaddr{l.localMultiaddr}
}

func (l *listener) handleCandidate(ctx context.Context, addr candidateAddr) (tpt.CapableConn, error) {
	remoteMultiaddr, err := manet.FromNetAddr(addr.raddr)
	if err != nil {
		return nil, err
	}
	scope, err := l.transport.rcmgr.OpenConnection(network.DirInbound, false, remoteMultiaddr)
	if err != nil {
		return nil, err
	}
	pc, conn, err := l.setupConnection(ctx, scope, remoteMultiaddr, addr)
	if err != nil {
		scope.Done()
		if pc != nil {
			_ = pc.Close()
		}
		return nil, err
	}
	return conn, nil
}

func (l *listener) setupConnection(ctx context.Context, scope network.ConnManagementScope, remoteMultiaddr ma.Multiaddr, addr candidateAddr) (*webrtc.PeerConnection, tpt.CapableConn, error) {

	settingEngine := webrtc.SettingEngine{}
	settingEngine.SetAnsweringDTLSRole(webrtc.DTLSRoleServer)
	settingEngine.SetICECredentials(addr.ufrag, addr.ufrag)
	settingEngine.SetLite(true)
	settingEngine.SetICEUDPMux(l.mux)
	settingEngine.DisableCertificateFingerprintVerification(true)
	settingEngine.DetachDataChannels()

	api := webrtc.NewAPI(webrtc.WithSettingEngine(settingEngine))

	pc, err := api.NewPeerConnection(l.config)
	if err != nil {
		return pc, nil, err
	}

	signalChan := make(chan error)
	var wrappedChannel *dataChannel
	var handshakeOnce sync.Once

	handshakeChannel, err := pc.CreateDataChannel("", &webrtc.DataChannelInit{
		Negotiated: func(v bool) *bool { return &v }(true),
		ID:         func(v uint16) *uint16 { return &v }(0),
	})
	if err != nil {
		return pc, nil, err
	}

	handshakeChannel.OnOpen(func() {
		rwc, err := handshakeChannel.Detach()
		if err != nil {
			signalChan <- errDatachannel("could not detach", err)
			return
		}
		wrappedChannel = newDataChannel(
			handshakeChannel,
			rwc,
			pc,
			l.localAddr,
			addr.raddr,
		)
		handshakeOnce.Do(func() {
			signalChan <- nil
		})
	})

	// Checking the peerconnection state is not necessary in this case as any
	// error caused while accepting will trigger the onerror callback of the
	// handshake channel.
	handshakeChannel.OnError(func(e error) {
		handshakeOnce.Do(func() {
			signalChan <- e

		})
	})

	clientSdpString := renderClientSdp(sdpArgs{
		Addr:  addr.raddr,
		Ufrag: addr.ufrag,
	})
	clientSdp := webrtc.SessionDescription{SDP: clientSdpString, Type: webrtc.SDPTypeOffer}
	pc.SetRemoteDescription(clientSdp)

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return pc, nil, err
	}

	err = pc.SetLocalDescription(answer)
	if err != nil {
		return pc, nil, err
	}

	// await datachannel moving to open state
	select {
	case <-ctx.Done():
		return pc, nil, ctx.Err()
	case err := <-signalChan:
		if err != nil {
			return pc, nil, errDatachannel("datachannel error", err)
		}
	}

	// The connection is instantiated before performing the Noise handshake. This is
	// to handle the case where the remote is faster and attempts to initiate a stream
	// before the ondatachannel callback can be set.
	conn := newConnection(
		pc,
		l.transport,
		scope,
		l.transport.localPeerId,
		l.transport.privKey,
		l.localMultiaddr,
		"",
		nil,
		remoteMultiaddr,
	)

	// we do not yet know A's peer ID so accept any inbound
	secureConn, err := l.transport.noiseHandshake(ctx, pc, wrappedChannel, "", crypto.SHA256, true)
	if err != nil {
		return pc, nil, err
	}

	// earliest point where we know the remote's peerID
	err = scope.SetPeer(secureConn.RemotePeer())
	if err != nil {
		return pc, nil, err
	}

	conn.setRemotePeer(secureConn.RemotePeer())
	conn.setRemotePublicKey(secureConn.RemotePublicKey())

	return pc, conn, err
}
