package ws

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"net/http"
	"sync"

	"github.com/libp2p/go-libp2p-core/introspection"
	pb "github.com/libp2p/go-libp2p-core/introspection/pb"

	"github.com/benbjohnson/clock"
	"github.com/gorilla/websocket"
	logging "github.com/ipfs/go-log"
)

var (
	logger   = logging.Logger("introspection/ws-server")
	upgrader = websocket.Upgrader{}
)

type sessionEvent struct {
	session *session
	doneCh  chan struct{}
}

type Server struct {
	// state initialized by constructor
	introspector introspection.Introspector
	config       *ServerConfig
	server       *http.Server
	clock        clock.Clock

	sessions map[*session]struct{}

	// state managed in the event loop
	sessionOpenedCh chan *sessionEvent
	sessionClosedCh chan *sessionEvent
	getSessionsCh   chan chan []*introspection.Session
	killSessionsCh  chan struct{}

	evalForTest chan func()

	// state managed by locking
	lk        sync.RWMutex
	listeners []net.Listener

	connsWg   sync.WaitGroup
	controlWg sync.WaitGroup

	closeCh  chan struct{}
	isClosed bool
}

var _ introspection.Endpoint = (*Server)(nil)

type ServerConfig struct {
	ListenAddrs []string
	Clock       clock.Clock
}

// ServerWithConfig returns a function compatible with the
// libp2p.Introspection constructor option, which when called, creates a
// Server with the supplied configuration.
func ServerWithConfig(config *ServerConfig) func(i introspection.Introspector) (introspection.Endpoint, error) {
	return func(i introspection.Introspector) (introspection.Endpoint, error) {
		return NewServer(i, config)
	}
}

// NewServer creates a WebSockets server to serve introspect data.
func NewServer(introspector introspection.Introspector, config *ServerConfig) (*Server, error) {
	if introspector == nil || config == nil {
		return nil, errors.New("none of introspect, event-bus OR config can be nil")
	}

	mux := http.NewServeMux()

	srv := &Server{
		introspector: introspector,
		server:       &http.Server{Handler: mux},
		config:       config,
		clock:        config.Clock,

		sessions:    make(map[*session]struct{}, 16),
		evalForTest: make(chan func()),

		sessionOpenedCh: make(chan *sessionEvent),
		sessionClosedCh: make(chan *sessionEvent),
		killSessionsCh:  make(chan struct{}),
		getSessionsCh:   make(chan chan []*introspection.Session),

		closeCh: make(chan struct{}),
	}

	if srv.clock == nil {
		// use the real clock.
		srv.clock = clock.New()
	}

	// register introspect session
	mux.HandleFunc("/introspect", srv.wsUpgrader())
	return srv, nil
}

// Start starts this WS server.
func (s *Server) Start() error {
	s.lk.Lock()
	defer s.lk.Unlock()

	if len(s.listeners) > 0 {
		return errors.New("failed to start WS server: already started")
	}
	if len(s.config.ListenAddrs) == 0 {
		return errors.New("failed to start WS server: no listen addresses supplied")
	}

	logger.Infof("WS introspection server starting, listening on %s", s.config.ListenAddrs)

	for _, addr := range s.config.ListenAddrs {
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("failed to start WS server: %wsvc", err)
		}

		go func() {
			if err := s.server.Serve(l); err != http.ErrServerClosed {
				logger.Errorf("failed to start WS server, err: %s", err)
			}
		}()

		s.listeners = append(s.listeners, l)
	}

	// start the worker
	s.controlWg.Add(1)
	go s.worker()

	return nil
}

// Close closes a WS introspect server.
func (s *Server) Close() error {
	s.lk.Lock()
	defer s.lk.Unlock()

	if s.isClosed {
		return nil
	}

	close(s.killSessionsCh)

	// wait for all connections to be dead.
	s.connsWg.Wait()

	close(s.closeCh)

	// Close the server, which in turn closes all listeners.
	if err := s.server.Close(); err != nil {
		return err
	}

	// cancel the context and wait for all goroutines to shut down
	s.controlWg.Wait()

	s.listeners = nil
	s.sessions = nil
	s.isClosed = true
	return nil
}

// ListenAddrs returns the actual listen addresses of this server.
func (s *Server) ListenAddrs() []string {
	s.lk.RLock()
	defer s.lk.RUnlock()

	res := make([]string, 0, len(s.listeners))
	for _, l := range s.listeners {
		res = append(res, l.Addr().String())
	}
	return res
}

func (s *Server) Sessions() []*introspection.Session {
	ch := make(chan []*introspection.Session)
	s.getSessionsCh <- ch
	return <-ch
}

func (s *Server) wsUpgrader() http.HandlerFunc {
	return func(w http.ResponseWriter, rq *http.Request) {
		upgrader.CheckOrigin = func(rq *http.Request) bool { return true }
		wsconn, err := upgrader.Upgrade(w, rq, nil)
		if err != nil {
			logger.Errorf("upgrade to websocket failed, err: %s", err)
			return
		}

		done := make(chan struct{}, 1)
		select {
		case s.sessionOpenedCh <- &sessionEvent{newSession(s, wsconn), done}:
		case <-s.closeCh:
			_ = wsconn.Close()
			return
		}

		select {
		case <-done:
		case <-s.closeCh:
			_ = wsconn.Close()
			return
		}
	}
}

func (s *Server) worker() {
	defer s.controlWg.Done()

	eventCh := s.introspector.EventChan()
	for {
		select {
		case rq := <-s.sessionOpenedCh:
			session := rq.session
			s.sessions[session] = struct{}{}

			s.connsWg.Add(1)
			go func() {
				session.run()

				select {
				case s.sessionClosedCh <- &sessionEvent{session, rq.doneCh}:
				case <-s.closeCh:
					return
				}
			}()

		case rq := <-s.sessionClosedCh:
			delete(s.sessions, rq.session)
			s.connsWg.Done()

		case evt, more := <-eventCh:
			if !more {
				eventCh = nil
				continue
			}

			if len(s.sessions) == 0 {
				continue
			}

			// generate the event and broadcast it to all sessions.
			if err := s.broadcastEvent(evt); err != nil {
				logger.Warnf("error while broadcasting event; err: %s", err)
			}

		case fnc := <-s.evalForTest:
			fnc()

		case ch := <-s.getSessionsCh:
			sessions := make([]*introspection.Session, 0, len(s.sessions))
			for sess := range s.sessions {
				sessions = append(sessions, &introspection.Session{RemoteAddr: sess.wsconn.RemoteAddr().String()})
			}
			ch <- sessions

		case <-s.killSessionsCh:
			for sess := range s.sessions {
				sess.kill()
			}

		case <-s.closeCh:
			return
		}
	}
}

func (s *Server) broadcastEvent(evt *pb.Event) error {
	pkt := &pb.ServerMessage{
		Version: introspection.ProtoVersionPb,
		Payload: &pb.ServerMessage_Event{Event: evt},
	}

	msg, err := envelopePacket(pkt)
	if err != nil {
		return fmt.Errorf("failed to generate enveloped event message; err: %w", err)
	}

	for sess := range s.sessions {
		sess.trySendEvent(msg)
	}

	return nil
}

func (s *Server) createStateMsg() ([]byte, error) {
	st, err := s.introspector.FetchFullState()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch state, err=%s", err)
	}

	pkt := &pb.ServerMessage{
		Version: introspection.ProtoVersionPb,
		Payload: &pb.ServerMessage_State{State: st},
	}

	return envelopePacket(pkt)
}

func (s *Server) createRuntimeMsg() ([]byte, error) {
	rt, err := s.introspector.FetchRuntime()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch runtime mesage, err=%s", err)
	}

	rt.EventTypes = s.introspector.EventMetadata()
	pkt := &pb.ServerMessage{
		Version: introspection.ProtoVersionPb,
		Payload: &pb.ServerMessage_Runtime{Runtime: rt},
	}

	return envelopePacket(pkt)
}

func envelopePacket(pkt *pb.ServerMessage) ([]byte, error) {
	// TODO buffer pool.
	size := pkt.Size()
	buf := make([]byte, 12+size)
	if _, err := pkt.MarshalToSizedBuffer(buf[12:]); err != nil {
		return nil, err
	}

	f := fnv.New32a()
	_, err := f.Write(buf[12:])
	if err != nil {
		return nil, fmt.Errorf("failed creating fnc hash digest, err: %w", err)
	}

	binary.LittleEndian.PutUint32(buf[0:4], introspection.ProtoVersion)
	binary.LittleEndian.PutUint32(buf[4:8], f.Sum32())
	binary.LittleEndian.PutUint32(buf[8:12], uint32(size))

	return buf, nil
}
