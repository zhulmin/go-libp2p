package introspect

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"time"

	"github.com/libp2p/go-eventbus"
	"github.com/libp2p/go-libp2p-core/event"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/introspection"
	pb "github.com/libp2p/go-libp2p-core/introspection/pb"
	"github.com/libp2p/go-libp2p-core/metrics"
	"github.com/libp2p/go-libp2p-core/network"
)

var _ introspection.Introspector = (*DefaultIntrospector)(nil)

// DefaultIntrospector is an object that introspects the system.
type DefaultIntrospector struct {
	*eventManager

	host     host.Host
	bus      event.Bus
	wsub     event.Subscription
	reporter metrics.Reporter
	started  time.Time

	closeCh chan struct{}
	closeWg sync.WaitGroup
}

func NewDefaultIntrospector(host host.Host, reporter metrics.Reporter) (*DefaultIntrospector, error) {
	bus := host.EventBus()
	if bus == nil {
		return nil, fmt.Errorf("introspector requires a host with eventbus capability")
	}

	sub, err := bus.Subscribe(event.WildcardSubscription, eventbus.BufSize(256))
	if err != nil {
		return nil, fmt.Errorf("failed to susbcribe for events with WildcardSubscription")
	}

	d := &DefaultIntrospector{
		eventManager: newEventManager(sub.Out()),
		host:         host,
		bus:          bus,
		wsub:         sub,
		reporter:     reporter,
		started:      time.Now(),
		closeCh:      make(chan struct{}),
	}

	d.closeWg.Add(1)
	go d.processEvents()
	return d, nil
}

func (d *DefaultIntrospector) Close() error {
	if err := d.wsub.Close(); err != nil {
		return fmt.Errorf("failed while trying to close wildcard eventbus subscription: %w", err)
	}

	close(d.closeCh)
	d.closeWg.Wait()

	return nil
}

func (d *DefaultIntrospector) FetchRuntime() (*pb.Runtime, error) {
	return &pb.Runtime{
		Implementation: "go-libp2p",
		Platform:       runtime.GOOS,
		PeerId:         d.host.ID().Pretty(),
		Version:        "",
	}, nil
}

func (d *DefaultIntrospector) FetchFullState() (state *pb.State, err error) {
	s := &pb.State{}

	// timestamps
	s.StartTs = timeToUnixMillis(d.started)
	s.InstantTs = timeToUnixMillis(time.Now())
	d.started = time.Now()

	// subsystems
	s.Subsystems = &pb.Subsystems{}
	s.Traffic, err = d.IntrospectGlobalTraffic()
	if err != nil {
		return nil, err
	}

	conns := d.host.Network().Conns()
	s.Subsystems.Connections = make([]*pb.Connection, 0, len(conns))
	for _, conn := range conns {
		c, err := d.IntrospectConnection(conn)
		if err != nil {
			return nil, err
		}
		s.Subsystems.Connections = append(s.Subsystems.Connections, c)
	}

	return s, nil
}

// IntrospectGlobalTraffic introspects and returns total traffic stats for this swarm.
func (d *DefaultIntrospector) IntrospectGlobalTraffic() (*pb.Traffic, error) {
	if d.reporter == nil {
		return nil, nil
	}

	metrics := d.reporter.GetBandwidthTotals()
	t := &pb.Traffic{
		TrafficIn: &pb.DataGauge{
			CumBytes: uint64(metrics.TotalIn),
			InstBw:   uint64(metrics.RateIn),
		},
		TrafficOut: &pb.DataGauge{
			CumBytes: uint64(metrics.TotalOut),
			InstBw:   uint64(metrics.RateOut),
		},
	}

	return t, nil
}

func (d *DefaultIntrospector) IntrospectConnection(conn network.Conn) (*pb.Connection, error) {
	stat := conn.Stat()
	openTs := uint64(stat.Opened.UnixNano() / 1000000)

	res := &pb.Connection{
		Id:     []byte(conn.ID()),
		Status: pb.Status_ACTIVE,
		PeerId: conn.RemotePeer().Pretty(),
		Endpoints: &pb.EndpointPair{
			SrcMultiaddr: conn.LocalMultiaddr().String(),
			DstMultiaddr: conn.RemoteMultiaddr().String(),
		},
		Role: translateRole(stat),

		Timeline: &pb.Connection_Timeline{
			OpenTs:     openTs,
			UpgradedTs: openTs,
			// TODO ClosedTs, UpgradedTs.
		},
	}

	// TODO this is a per-peer, not a per-conn measurement. In the future, when
	//  we have multiple connections per peer, this will produce inaccurate
	//  numbers. Also, we do not record stream-level stats.
	//  We don't have packet I/O stats.
	if r := d.reporter; r != nil {
		bw := r.GetBandwidthForPeer(conn.RemotePeer())
		res.Traffic = &pb.Traffic{
			TrafficIn: &pb.DataGauge{
				CumBytes: uint64(bw.TotalIn),
				InstBw:   uint64(math.Round(bw.RateIn)),
			},
			TrafficOut: &pb.DataGauge{
				CumBytes: uint64(bw.TotalOut),
				InstBw:   uint64(math.Round(bw.RateOut)),
			},
		}
	}

	// TODO I don't think we pin the multiplexer and the secure channel we've
	//  negotiated anywhere.
	res.Attribs = &pb.Connection_Attributes{}

	// TODO can we get the transport ID from the multiaddr?
	res.TransportId = nil

	// TODO there's the ping protocol, but that's higher than this layer.
	//  How do we source this? We may need some kind of latency manager.
	res.LatencyNs = 0

	streams := conn.GetStreams()
	res.Streams = &pb.StreamList{
		Streams: make([]*pb.Stream, 0, len(streams)),
	}

	for _, stream := range streams {
		s, err := d.IntrospectStream(stream)
		if err != nil {
			return nil, err
		}
		res.Streams.Streams = append(res.Streams.Streams, s)
	}

	return res, nil
}

func (d *DefaultIntrospector) IntrospectStream(stream network.Stream) (*pb.Stream, error) {
	stat := stream.Stat()
	openTs := uint64(stat.Opened.UnixNano() / 1000000)

	res := &pb.Stream{
		Id:     []byte(stream.ID()),
		Status: pb.Status_ACTIVE,
		Conn: &pb.Stream_ConnectionRef{
			Connection: &pb.Stream_ConnectionRef_ConnId{
				ConnId: []byte(stream.Conn().ID()),
			},
		},
		Protocol: string(stream.Protocol()),
		Role:     translateRole(stat),
		Timeline: &pb.Stream_Timeline{
			OpenTs: openTs,
			// TODO CloseTs.
		},
		// TODO Traffic: we are not tracking per-stream traffic stats at the
		Traffic: &pb.Traffic{TrafficIn: &pb.DataGauge{}, TrafficOut: &pb.DataGauge{}},
		// moment.
	}
	return res, nil
}

func translateRole(stat network.Stat) pb.Role {
	switch stat.Direction {
	case network.DirInbound:
		return pb.Role_RESPONDER
	case network.DirOutbound:
		return pb.Role_INITIATOR
	default:
		return 99 // TODO placeholder value
	}
}

func timeToUnixMillis(t time.Time) uint64 {
	return uint64(t.UnixNano() / 1000000)
}
