package swarm

import (
	"context"

	"github.com/libp2p/go-libp2p/core/network"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

const metricNamespace = "swarm/"

var (
	connsOpened = stats.Int64(metricNamespace+"connections_opened", "Connections Opened", stats.UnitDimensionless)
	connsClosed = stats.Int64(metricNamespace+"connections_closed", "Connections Closed", stats.UnitDimensionless)
)

var (
	directionTag, _ = tag.NewKey("dir")
	transportTag, _ = tag.NewKey("transport")
	securityTag, _  = tag.NewKey("security")
	muxerTag, _     = tag.NewKey("muxer")
)

var (
	connOpenView   = &view.View{Measure: connsOpened, Aggregation: view.Sum(), TagKeys: []tag.Key{directionTag, transportTag, securityTag, muxerTag}}
	connClosedView = &view.View{Measure: connsClosed, Aggregation: view.Sum(), TagKeys: []tag.Key{directionTag, transportTag, securityTag, muxerTag}}
)

var DefaultViews = []*view.View{connOpenView, connClosedView}

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

func recordConnectionOpened(dir network.Direction, cs network.ConnectionState) {
	tags := make([]tag.Mutator, 0, 4)
	tags = append(tags, tag.Upsert(directionTag, getDirection(dir)))
	tags = appendConnectionState(tags, cs)
	stats.RecordWithTags(context.Background(), tags, connsOpened.M(1))
}

func recordConnectionClosed(dir network.Direction, cs network.ConnectionState) {
	tags := make([]tag.Mutator, 0, 4)
	tags = append(tags, tag.Upsert(directionTag, getDirection(dir)))
	tags = appendConnectionState(tags, cs)
	stats.RecordWithTags(context.Background(), tags, connsClosed.M(1))
}
