package swarm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"

	ma "github.com/multiformats/go-multiaddr"

	"github.com/libp2p/go-libp2p/core/network"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

const metricNamespace = "swarm/"

var (
	connsOpened          = stats.Int64(metricNamespace+"connections_opened", "Connections Opened", stats.UnitDimensionless)
	connsClosed          = stats.Int64(metricNamespace+"connections_closed", "Connections Closed", stats.UnitDimensionless)
	keyType              = stats.Int64(metricNamespace+"key_type", "libp2p key type", stats.UnitDimensionless)
	dialError            = stats.Int64(metricNamespace+"dial_error", "Dial Error", stats.UnitDimensionless)
	connDuration         = stats.Float64(metricNamespace+"connection_duration", "Duration of a Connection", stats.UnitSeconds)
	connHandshakeLatency = stats.Float64(metricNamespace+"handshake_latency", "Duration of the libp2p handshake", stats.UnitSeconds)
)

var (
	directionTag, _ = tag.NewKey("dir")
	transportTag, _ = tag.NewKey("transport")
	securityTag, _  = tag.NewKey("security")
	muxerTag, _     = tag.NewKey("muxer")
	dialErrorTag, _ = tag.NewKey("dial_error")
	keyTypeTag, _   = tag.NewKey("key_type")
)

var transports = [...]int{ma.P_CIRCUIT, ma.P_WEBRTC, ma.P_WEBTRANSPORT, ma.P_QUIC, ma.P_QUIC_V1, ma.P_WSS, ma.P_WS, ma.P_TCP}

func exponentialDistribution(min, max float64) []float64 {
	var v []float64
	for d := min; d < 2*max; d *= 2 {
		v = append(v, d)
	}
	return v
}

func getHandshakeLatencyBuckes() []float64 {
	var buckets []float64
	for i := 0.01; i <= 1; i += 0.02 {
		buckets = append(buckets, i)
	}
	for i := 1.1; i <= 5; i += 0.1 {
		buckets = append(buckets, i)
	}
	for i := 5.25; i <= 10; i += 0.25 {
		buckets = append(buckets, i)
	}
	return buckets
}

var (
	handshakeLatencySeconds = getHandshakeLatencyBuckes()
	connDurationSeconds     = exponentialDistribution(250, (7 * 24 * time.Hour).Seconds())
)

var (
	connOpenView = &view.View{
		Measure:     connsOpened,
		Aggregation: view.Sum(),
		TagKeys:     []tag.Key{directionTag, transportTag, securityTag, muxerTag},
	}
	connClosedView = &view.View{
		Measure:     connsClosed,
		Aggregation: view.Sum(),
		TagKeys:     []tag.Key{directionTag, transportTag, securityTag, muxerTag},
	}
	keyTypeView = &view.View{
		Measure:     keyType,
		Aggregation: view.Sum(),
		TagKeys:     []tag.Key{keyTypeTag},
	}
	dialErrorView = &view.View{
		Measure:     dialError,
		Aggregation: view.Sum(),
		TagKeys:     []tag.Key{transportTag, dialErrorTag},
	}
	connDurationView = &view.View{
		Measure:     connDuration,
		Aggregation: view.Distribution(connDurationSeconds...),
		TagKeys:     []tag.Key{directionTag, transportTag, securityTag, muxerTag},
	}
	connHandshakeLatencyView = &view.View{
		Measure:     connHandshakeLatency,
		Aggregation: view.Distribution(handshakeLatencySeconds...),
		TagKeys:     []tag.Key{transportTag, securityTag, muxerTag},
	}
)

var DefaultViews = []*view.View{connOpenView, connClosedView, keyTypeView, dialErrorView, connDurationView, connHandshakeLatencyView}

func getDirection(dir network.Direction) string {
	switch dir {
	case network.DirOutbound:
		return "outbound"
	case network.DirInbound:
		return "inbound"
	default:
		return "unknown"
	}
}

func appendConnectionState(tags []tag.Mutator, cs network.ConnectionState) []tag.Mutator {
	if cs.Transport == "" {
		// This shouldn't happen, unless the transport doesn't properly set the Transport field in the ConnectionState.
		tags = append(tags, tag.Upsert(transportTag, "unknown"))
	} else {
		tags = append(tags, tag.Upsert(transportTag, cs.Transport))
	}
	// Only set the security and muxer tag for transports that actually use that field.
	// For example, QUIC doesn't set security nor muxer.
	if cs.Security != "" {
		tags = append(tags, tag.Upsert(securityTag, cs.Security))
	}
	if cs.StreamMultiplexer != "" {
		tags = append(tags, tag.Upsert(muxerTag, cs.StreamMultiplexer))
	}
	return tags
}

func recordConnectionOpened(dir network.Direction, p crypto.PubKey, cs network.ConnectionState) {
	tags := make([]tag.Mutator, 0, 4)
	tags = append(tags, tag.Upsert(directionTag, getDirection(dir)))
	tags = appendConnectionState(tags, cs)
	stats.RecordWithTags(context.Background(), tags, connsOpened.M(1))
	stats.RecordWithTags(context.Background(), []tag.Mutator{tag.Upsert(keyTypeTag, p.Type().String())}, keyType.M(1))
}

func recordConnectionClosed(dir network.Direction, cs network.ConnectionState) {
	tags := make([]tag.Mutator, 0, 4)
	tags = append(tags, tag.Upsert(directionTag, getDirection(dir)))
	tags = appendConnectionState(tags, cs)
	stats.RecordWithTags(context.Background(), tags, connsClosed.M(1))
}

func recordConnectionDuration(dir network.Direction, t time.Duration, cs network.ConnectionState) {
	tags := make([]tag.Mutator, 0, 4)
	tags = append(tags, tag.Upsert(directionTag, getDirection(dir)))
	tags = appendConnectionState(tags, cs)
	stats.RecordWithTags(context.Background(), tags, connDuration.M(t.Seconds()))
}

func recordHandshakeLatency(t time.Duration, cs network.ConnectionState) {
	tags := make([]tag.Mutator, 0, 3)
	tags = appendConnectionState(tags, cs)
	stats.RecordWithTags(context.Background(), tags, connHandshakeLatency.M(t.Seconds()))
}

func recordDialFailed(addr ma.Multiaddr, err error) {
	var transport string
	for _, p := range transports {
		if _, err := addr.ValueForProtocol(p); err == nil {
			transport = ma.ProtocolWithCode(p).Name
			break
		}
	}
	e := "other"
	if errors.Is(err, context.Canceled) {
		e = "canceled"
	} else if errors.Is(err, context.DeadlineExceeded) {
		e = "deadline"
	} else {
		nerr, ok := err.(net.Error)
		if ok && nerr.Timeout() {
			e = "timeout"
		} else if strings.Contains(err.Error(), "connect: connection refused") {
			e = "connection refused"
		}
	}
	if e == "other" {
		fmt.Printf("transport: %s, category: %s (orig: %s)\n", transport, e, err)
	}
	stats.RecordWithTags(context.Background(), []tag.Mutator{tag.Upsert(transportTag, transport), tag.Upsert(dialErrorTag, e)}, dialError.M(1))
}
