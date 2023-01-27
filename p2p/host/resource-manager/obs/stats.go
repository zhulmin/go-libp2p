package obs

import (
	"context"
	"strings"

	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/prometheus/client_golang/prometheus"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	metricNamespaceOld = "rcmgr_deprecated/"
	connsOld           = stats.Int64(metricNamespaceOld+"connections", "Number of Connections", stats.UnitDimensionless)

	metricNamespace = "rcmgr"

	// Conns
	conns = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Name:      "connections",
		Help:      "Number of Connections",
	}, []string{"dir", "scope"})

	connsInboundSystem     = conns.With(prometheus.Labels{"dir": "inbound", "scope": "system"})
	connsInboundTransient  = conns.With(prometheus.Labels{"dir": "inbound", "scope": "transient"})
	connsOutboundSystem    = conns.With(prometheus.Labels{"dir": "outbound", "scope": "system"})
	connsOutboundTransient = conns.With(prometheus.Labels{"dir": "outbound", "scope": "transient"})

	oneTenThenExpDistributionBuckets = []float64{
		1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 16, 32, 64, 128, 256,
	}

	// PeerConns
	peerConns = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricNamespace,
		Name:      "peer_connections",
		Buckets:   oneTenThenExpDistributionBuckets,
		Help:      "Number of connections this peer has",
	}, []string{"dir"})
	peerConnsInbound  = peerConns.With(prometheus.Labels{"dir": "inbound"})
	peerConnsOutbound = peerConns.With(prometheus.Labels{"dir": "outbound"})

	// Lets us build a histogram of our current state. See https://github.com/libp2p/go-libp2p-resource-manager/pull/54#discussion_r911244757 for more information.
	previousPeerConns = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricNamespace,
		Name:      "previous_peer_connections",
		Buckets:   oneTenThenExpDistributionBuckets,
		Help:      "Number of connections this peer previously had. This is used to get the current connection number per peer histogram by subtracting this from the peer_connections histogram",
	}, []string{"dir"})
	previousPeerConnsInbound  = previousPeerConns.With(prometheus.Labels{"dir": "inbound"})
	previousPeerConnsOutbound = previousPeerConns.With(prometheus.Labels{"dir": "outbound"})

	// Streams
	streams = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Name:      "streams",
		Help:      "Number of Streams",
	}, []string{"dir", "scope", "protocol"})

	peerStreams = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricNamespace,
		Name:      "peer_streams",
		Buckets:   oneTenThenExpDistributionBuckets,
		Help:      "Number of streams this peer has",
	}, []string{"dir"})
	peerStreamsInbound  = peerStreams.With(prometheus.Labels{"dir": "inbound"})
	peerStreamsOutbound = peerStreams.With(prometheus.Labels{"dir": "outbound"})

	previousPeerStreams = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metricNamespace,
		Name:      "previous_peer_streams",
		Buckets:   oneTenThenExpDistributionBuckets,
		Help:      "Number of streams this peer has",
	}, []string{"dir"})
	previousPeerStreamsInbound  = previousPeerStreams.With(prometheus.Labels{"dir": "inbound"})
	previousPeerStreamsOutbound = previousPeerStreams.With(prometheus.Labels{"dir": "outbound"})

	// Memory

	memory = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Name:      "memory",
		Help:      "Amount of memory reserved as reported to the Resource Manager",
	}, []string{"scope", "protocol"})

	// PeerMemory
	peerMemory = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricNamespace,
		Name:      "peer_memory",
		Buckets:   memDistribution,
		// Help:      "Amount of memory reserved as reported to the Resource Manager",
	})
	previousPeerMemory = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricNamespace,
		Name:      "previous_peer_memory",
		Buckets:   memDistribution,
		// Help:      "Amount of memory reserved as reported to the Resource Manager",
	})

	// ConnMemory
	connMemory = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricNamespace,
		Name:      "conn_memory",
		Buckets:   memDistribution,
		// Help:      "Amount of memory reserved as reported to the Resource Manager",
	})
	previousConnMemory = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricNamespace,
		Name:      "previous_conn_memory",
		Buckets:   memDistribution,
		// Help:      "Amount of memory reserved as reported to the Resource Manager",
	})

	// FDs

	fds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Name:      "fds",
		Help:      "Number of file descriptors reserved as reported to the Resource Manager",
	}, []string{"scope"})

	fdsSystem    = fds.With(prometheus.Labels{"scope": "system"})
	fdsTransient = fds.With(prometheus.Labels{"scope": "transient"})

	// Blocked resources

	blockedResources = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Name:      "blocked_resources",
		Help:      "Number of blocked resources",
	}, []string{"direction", "scope", "resource"})

	// Old

	peerConnsOld         = stats.Int64(metricNamespaceOld+"peer/connections", "Number of connections this peer has", stats.UnitDimensionless)
	peerConnsNegativeOld = stats.Int64(metricNamespaceOld+"peer/connections_negative", "Number of connections this peer had. This is used to get the current connection number per peer histogram by subtracting this from the peer/connections histogram", stats.UnitDimensionless)

	streamsOld = stats.Int64(metricNamespaceOld+"streams", "Number of Streams", stats.UnitDimensionless)

	peerStreamsOld      = stats.Int64(metricNamespaceOld+"peer/streams", "Number of streams this peer has", stats.UnitDimensionless)
	peerStreamsNegative = stats.Int64(metricNamespaceOld+"peer/streams_negative", "Number of streams this peer had. This is used to get the current streams number per peer histogram by subtracting this from the peer/streams histogram", stats.UnitDimensionless)

	memoryOld          = stats.Int64(metricNamespaceOld+"memory", "Amount of memory reserved as reported to the Resource Manager", stats.UnitDimensionless)
	peerMemoryOld      = stats.Int64(metricNamespaceOld+"peer/memory", "Amount of memory currently reseved for peer", stats.UnitDimensionless)
	peerMemoryNegative = stats.Int64(metricNamespaceOld+"peer/memory_negative", "Amount of memory previously reseved for peer. This is used to get the current memory per peer histogram by subtracting this from the peer/memory histogram", stats.UnitDimensionless)

	connMemoryOld      = stats.Int64(metricNamespaceOld+"conn/memory", "Amount of memory currently reseved for the connection", stats.UnitDimensionless)
	connMemoryNegative = stats.Int64(metricNamespaceOld+"conn/memory_negative", "Amount of memory previously reseved for the connection.  This is used to get the current memory per connection histogram by subtracting this from the conn/memory histogram", stats.UnitDimensionless)

	fdsOld = stats.Int64(metricNamespaceOld+"fds", "Number of fds as reported to the Resource Manager", stats.UnitDimensionless)

	blockedResourcesOld = stats.Int64(metricNamespaceOld+"blocked_resources", "Number of resource requests blocked", stats.UnitDimensionless)
)

var (
	directionTag, _ = tag.NewKey("dir")
	scopeTag, _     = tag.NewKey("scope")
	serviceTag, _   = tag.NewKey("service")
	protocolTag, _  = tag.NewKey("protocol")
	resourceTag, _  = tag.NewKey("resource")
)

var (
	ConnView = &view.View{Measure: connsOld, Aggregation: view.Sum(), TagKeys: []tag.Key{directionTag, scopeTag}}

	oneTenThenExpDistribution = []float64{
		1.1, 2.1, 3.1, 4.1, 5.1, 6.1, 7.1, 8.1, 9.1, 10.1, 16.1, 32.1, 64.1, 128.1, 256.1,
	}

	PeerConnsView = &view.View{
		Measure:     peerConnsOld,
		Aggregation: view.Distribution(oneTenThenExpDistribution...),
		TagKeys:     []tag.Key{directionTag},
	}
	PeerConnsNegativeView = &view.View{
		Measure:     peerConnsNegativeOld,
		Aggregation: view.Distribution(oneTenThenExpDistribution...),
		TagKeys:     []tag.Key{directionTag},
	}

	StreamView             = &view.View{Measure: streamsOld, Aggregation: view.Sum(), TagKeys: []tag.Key{directionTag, scopeTag, serviceTag, protocolTag}}
	PeerStreamsView        = &view.View{Measure: peerStreamsOld, Aggregation: view.Distribution(oneTenThenExpDistribution...), TagKeys: []tag.Key{directionTag}}
	PeerStreamNegativeView = &view.View{Measure: peerStreamsNegative, Aggregation: view.Distribution(oneTenThenExpDistribution...), TagKeys: []tag.Key{directionTag}}

	MemoryView = &view.View{Measure: memoryOld, Aggregation: view.Sum(), TagKeys: []tag.Key{scopeTag, serviceTag, protocolTag}}

	memDistribution = []float64{
		1 << 10,   // 1KB
		4 << 10,   // 4KB
		32 << 10,  // 32KB
		1 << 20,   // 1MB
		32 << 20,  // 32MB
		256 << 20, // 256MB
		512 << 20, // 512MB
		1 << 30,   // 1GB
		2 << 30,   // 2GB
		4 << 30,   // 4GB
	}
	PeerMemoryView = &view.View{
		Measure:     peerMemoryOld,
		Aggregation: view.Distribution(memDistribution...),
	}
	PeerMemoryNegativeView = &view.View{
		Measure:     peerMemoryNegative,
		Aggregation: view.Distribution(memDistribution...),
	}

	// Not setup yet. Memory isn't attached to a given connection.
	ConnMemoryView = &view.View{
		Measure:     connMemoryOld,
		Aggregation: view.Distribution(memDistribution...),
	}
	ConnMemoryNegativeView = &view.View{
		Measure:     connMemoryNegative,
		Aggregation: view.Distribution(memDistribution...),
	}

	FDsView = &view.View{Measure: fdsOld, Aggregation: view.Sum(), TagKeys: []tag.Key{scopeTag}}

	BlockedResourcesView = &view.View{
		Measure:     blockedResourcesOld,
		Aggregation: view.Sum(),
		TagKeys:     []tag.Key{scopeTag, resourceTag},
	}
)

var DefaultViews []*view.View = []*view.View{
	ConnView,
	PeerConnsView,
	PeerConnsNegativeView,
	FDsView,

	StreamView,
	PeerStreamsView,
	PeerStreamNegativeView,

	MemoryView,
	PeerMemoryView,
	PeerMemoryNegativeView,

	BlockedResourcesView,
}

func MustRegisterWith(reg prometheus.Registerer) {
	reg.MustRegister(
		conns,
		peerConns,
		previousPeerConns,
		streams,
		peerStreams,

		previousPeerStreams,

		memory,
		peerMemory,
		previousPeerMemory,
		connMemory,
		previousConnMemory,
		fds,
		blockedResources,
	)
}

// StatsTraceReporter reports stats on the resource manager using its traces.
type StatsTraceReporter struct{}

func NewStatsTraceReporter() (StatsTraceReporter, error) {
	// TODO tell prometheus the system limits
	return StatsTraceReporter{}, nil
}

func (r StatsTraceReporter) ConsumeEvent(evt rcmgr.TraceEvt) {
	ctx := context.Background()

	switch evt.Type {
	case rcmgr.TraceAddStreamEvt, rcmgr.TraceRemoveStreamEvt:
		if p := rcmgr.ParsePeerScopeName(evt.Name); p.Validate() == nil {
			// Aggregated peer stats. Counts how many peers have N number of streams open.
			// Uses two buckets aggregations. One to count how many streams the
			// peer has now. The other to count the negative value, or how many
			// streams did the peer use to have. When looking at the data you
			// take the difference from the two.

			oldStreamsOut := int64(evt.StreamsOut - evt.DeltaOut)
			peerStreamsOut := int64(evt.StreamsOut)
			if oldStreamsOut != peerStreamsOut {
				if oldStreamsOut != 0 {
					stats.RecordWithTags(ctx, []tag.Mutator{tag.Upsert(directionTag, "outbound")}, peerStreamsNegative.M(oldStreamsOut))
					previousPeerStreamsOutbound.Observe(float64(oldStreamsOut))
				}
				if peerStreamsOut != 0 {
					stats.RecordWithTags(ctx, []tag.Mutator{tag.Upsert(directionTag, "outbound")}, peerStreamsOld.M(peerStreamsOut))
					peerStreamsOutbound.Observe(float64(peerStreamsOut))
				}
			}

			oldStreamsIn := int64(evt.StreamsIn - evt.DeltaIn)
			peerStreamsIn := int64(evt.StreamsIn)
			if oldStreamsIn != peerStreamsIn {
				if oldStreamsIn != 0 {
					stats.RecordWithTags(ctx, []tag.Mutator{tag.Upsert(directionTag, "inbound")}, peerStreamsNegative.M(oldStreamsIn))
					previousPeerStreamsInbound.Observe(float64(oldStreamsIn))
				}
				if peerStreamsIn != 0 {
					stats.RecordWithTags(ctx, []tag.Mutator{tag.Upsert(directionTag, "inbound")}, peerStreamsOld.M(peerStreamsIn))
					peerStreamsInbound.Observe(float64(peerStreamsIn))
				}
			}
		} else {
			var tags []tag.Mutator
			if rcmgr.IsSystemScope(evt.Name) || rcmgr.IsTransientScope(evt.Name) {
				tags = append(tags, tag.Upsert(scopeTag, evt.Name))
			} else if svc := rcmgr.ParseServiceScopeName(evt.Name); svc != "" {
				tags = append(tags, tag.Upsert(scopeTag, "service"), tag.Upsert(serviceTag, svc))
			} else if proto := rcmgr.ParseProtocolScopeName(evt.Name); proto != "" {
				tags = append(tags, tag.Upsert(scopeTag, "protocol"), tag.Upsert(protocolTag, proto))
			} else {
				// Not measuring connscope, servicepeer and protocolpeer. Lots of data, and
				// you can use aggregated peer stats + service stats to infer
				// this.
				break
			}

			if evt.DeltaOut != 0 {
				stats.RecordWithTags(
					ctx,
					append([]tag.Mutator{tag.Upsert(directionTag, "outbound")}, tags...),
					streamsOld.M(int64(evt.DeltaOut)),
				)

				if rcmgr.IsSystemScope(evt.Name) || rcmgr.IsTransientScope(evt.Name) {
					streams.With(prometheus.Labels{"dir": "outbound", "scope": evt.Name, "protocol": ""}).Set(float64(evt.StreamsOut))
				} else if proto := rcmgr.ParseProtocolScopeName(evt.Name); proto != "" {
					streams.With(prometheus.Labels{"dir": "outbound", "scope": "protocol", "protocol": proto}).Set(float64(evt.StreamsOut))
				} else {
					// Not measuring service scope, connscope, servicepeer and protocolpeer. Lots of data, and
					// you can use aggregated peer stats + service stats to infer
					// this.
					break
				}
			}

			if evt.DeltaIn != 0 {
				if rcmgr.IsSystemScope(evt.Name) || rcmgr.IsTransientScope(evt.Name) {
					streams.With(prometheus.Labels{"dir": "inbound", "scope": evt.Name, "protocol": ""}).Set(float64(evt.StreamsIn))
				} else if proto := rcmgr.ParseProtocolScopeName(evt.Name); proto != "" {
					streams.With(prometheus.Labels{"dir": "inbound", "scope": "protocol", "protocol": proto}).Set(float64(evt.StreamsIn))
				} else {
					// Not measuring service scope, connscope, servicepeer and protocolpeer. Lots of data, and
					// you can use aggregated peer stats + service stats to infer
					// this.
					break
				}

				stats.RecordWithTags(
					ctx,
					append([]tag.Mutator{tag.Upsert(directionTag, "inbound")}, tags...),
					streamsOld.M(int64(evt.DeltaIn)),
				)
			}
		}

	case rcmgr.TraceAddConnEvt, rcmgr.TraceRemoveConnEvt:
		if p := rcmgr.ParsePeerScopeName(evt.Name); p.Validate() == nil {
			// Aggregated peer stats. Counts how many peers have N number of connections.
			// Uses two buckets aggregations. One to count how many streams the
			// peer has now. The other to count the negative value, or how many
			// conns did the peer use to have. When looking at the data you
			// take the difference from the two.

			oldConnsOut := int64(evt.ConnsOut - evt.DeltaOut)
			connsOut := int64(evt.ConnsOut)
			if oldConnsOut != connsOut {
				if oldConnsOut != 0 {
					previousPeerConnsOutbound.Observe(float64(oldConnsOut))
					stats.RecordWithTags(ctx, []tag.Mutator{tag.Upsert(directionTag, "outbound")}, peerConnsNegativeOld.M(oldConnsOut))
				}
				if connsOut != 0 {
					peerConnsOutbound.Observe(float64(connsOut))
					stats.RecordWithTags(ctx, []tag.Mutator{tag.Upsert(directionTag, "outbound")}, peerConnsOld.M(connsOut))
				}
			}

			oldConnsIn := int64(evt.ConnsIn - evt.DeltaIn)
			connsIn := int64(evt.ConnsIn)
			if oldConnsIn != connsIn {
				if oldConnsIn != 0 {
					previousPeerConnsInbound.Observe(float64(oldConnsIn))
					stats.RecordWithTags(ctx, []tag.Mutator{tag.Upsert(directionTag, "inbound")}, peerConnsNegativeOld.M(oldConnsIn))
				}
				if connsIn != 0 {
					peerConnsInbound.Observe(float64(connsIn))
					stats.RecordWithTags(ctx, []tag.Mutator{tag.Upsert(directionTag, "inbound")}, peerConnsOld.M(connsIn))
				}
			}
		} else {
			var tags []tag.Mutator
			if rcmgr.IsSystemScope(evt.Name) || rcmgr.IsTransientScope(evt.Name) {
				tags = append(tags, tag.Upsert(scopeTag, evt.Name))
			} else if rcmgr.IsConnScope(evt.Name) {
				// Not measuring this. I don't think it's useful.
				break
			} else {
				// This could be a span
				break
			}

			if rcmgr.IsSystemScope(evt.Name) {
				connsInboundSystem.Set(float64(evt.ConnsIn))
				connsOutboundSystem.Set(float64(evt.ConnsOut))
			} else if rcmgr.IsTransientScope(evt.Name) {
				connsInboundTransient.Set(float64(evt.ConnsIn))
				connsOutboundTransient.Set(float64(evt.ConnsOut))
			}

			if evt.DeltaOut != 0 {
				stats.RecordWithTags(
					ctx,
					append([]tag.Mutator{tag.Upsert(directionTag, "outbound")}, tags...),
					connsOld.M(int64(evt.DeltaOut)),
				)
			}

			if evt.DeltaIn != 0 {
				stats.RecordWithTags(
					ctx,
					append([]tag.Mutator{tag.Upsert(directionTag, "inbound")}, tags...),
					connsOld.M(int64(evt.DeltaIn)),
				)
			}

			// Represents the delta in fds
			if evt.Delta != 0 {
				if rcmgr.IsSystemScope(evt.Name) {
					fdsSystem.Set(float64(evt.FD))
				} else if rcmgr.IsTransientScope(evt.Name) {
					fdsTransient.Set(float64(evt.FD))
				}

				stats.RecordWithTags(
					ctx,
					tags,
					fdsOld.M(int64(evt.Delta)),
				)
			}
		}
	case rcmgr.TraceReserveMemoryEvt, rcmgr.TraceReleaseMemoryEvt:
		if p := rcmgr.ParsePeerScopeName(evt.Name); p.Validate() == nil {
			oldMem := evt.Memory - evt.Delta
			if oldMem != evt.Memory {
				if oldMem != 0 {
					stats.Record(ctx, peerMemoryNegative.M(oldMem))
					previousPeerMemory.Observe(float64(oldMem))
				}
				if evt.Memory != 0 {
					stats.Record(ctx, peerMemoryOld.M(evt.Memory))
					peerMemory.Observe(float64(evt.Memory))
				}
			}
		} else if rcmgr.IsConnScope(evt.Name) {
			oldMem := evt.Memory - evt.Delta
			if oldMem != evt.Memory {
				if oldMem != 0 {
					stats.Record(ctx, connMemoryNegative.M(oldMem))
					previousConnMemory.Observe(float64(oldMem))
				}
				if evt.Memory != 0 {
					stats.Record(ctx, connMemoryOld.M(evt.Memory))
					connMemory.Observe(float64(evt.Memory))
				}
			}
		} else {
			var tags []tag.Mutator
			if rcmgr.IsSystemScope(evt.Name) || rcmgr.IsTransientScope(evt.Name) {
				tags = append(tags, tag.Upsert(scopeTag, evt.Name))
			} else if svc := rcmgr.ParseServiceScopeName(evt.Name); svc != "" {
				tags = append(tags, tag.Upsert(scopeTag, "service"), tag.Upsert(serviceTag, svc))
			} else if proto := rcmgr.ParseProtocolScopeName(evt.Name); proto != "" {
				tags = append(tags, tag.Upsert(scopeTag, "protocol"), tag.Upsert(protocolTag, proto))
			} else {
				// Not measuring connscope, servicepeer and protocolpeer. Lots of data, and
				// you can use aggregated peer stats + service stats to infer
				// this.
				break
			}

			if evt.Delta != 0 {
				stats.RecordWithTags(ctx, tags, memoryOld.M(int64(evt.Delta)))
			}

			if rcmgr.IsSystemScope(evt.Name) || rcmgr.IsTransientScope(evt.Name) {
				memory.With(prometheus.Labels{"scope": evt.Name, "protocol": ""}).Set(float64(evt.Memory))
			} else if proto := rcmgr.ParseProtocolScopeName(evt.Name); proto != "" {
				memory.With(prometheus.Labels{"scope": "protocol", "protocol": proto}).Set(float64(evt.Memory))
			} else {
				// Not measuring connscope, servicepeer and protocolpeer. Lots of data, and
				// you can use aggregated peer stats + service stats to infer
				// this.
				break
			}

			if evt.Delta != 0 {
				stats.RecordWithTags(ctx, tags, memoryOld.M(int64(evt.Delta)))
				stats.RecordWithTags(ctx, tags, memoryOld.M(int64(evt.Delta)))
			}
		}

	case rcmgr.TraceBlockAddConnEvt, rcmgr.TraceBlockAddStreamEvt, rcmgr.TraceBlockReserveMemoryEvt:
		var resource string
		if evt.Type == rcmgr.TraceBlockAddConnEvt {
			resource = "connection"
		} else if evt.Type == rcmgr.TraceBlockAddStreamEvt {
			resource = "stream"
		} else {
			resource = "memory"
		}

		// Only the top scopeName. We don't want to get the peerid here.
		scopeName := strings.SplitN(evt.Name, ":", 2)[0]
		// Drop the connection or stream id
		scopeName = strings.SplitN(scopeName, "-", 2)[0]

		// If something else gets added here, make sure to update the size hint
		// below when we make `tagsWithDir`.
		tags := []tag.Mutator{tag.Upsert(scopeTag, scopeName), tag.Upsert(resourceTag, resource)}

		if evt.DeltaIn != 0 {
			tagsWithDir := make([]tag.Mutator, 0, 3)
			tagsWithDir = append(tagsWithDir, tag.Insert(directionTag, "inbound"))
			tagsWithDir = append(tagsWithDir, tags...)
			stats.RecordWithTags(ctx, tagsWithDir[0:], blockedResourcesOld.M(int64(1)))

			blockedResources.With(prometheus.Labels{"direction": "inbound", "scope": scopeName, "resource": resource}).Add(1)
		}

		if evt.DeltaOut != 0 {
			tagsWithDir := make([]tag.Mutator, 0, 3)
			tagsWithDir = append(tagsWithDir, tag.Insert(directionTag, "outbound"))
			tagsWithDir = append(tagsWithDir, tags...)
			stats.RecordWithTags(ctx, tagsWithDir, blockedResourcesOld.M(int64(1)))

			blockedResources.With(prometheus.Labels{"direction": "outbound", "scope": scopeName, "resource": resource}).Add(1)
		}

		if evt.Delta != 0 {
			stats.RecordWithTags(ctx, tags, blockedResourcesOld.M(1))
		}

		if evt.Delta != 0 && resource == "connection" {
			// This represents fds blocked
			blockedResources.With(prometheus.Labels{"direction": "", "scope": scopeName, "resource": "fd"}).Add(1)
		} else if evt.Delta != 0 {
			blockedResources.With(prometheus.Labels{"direction": "", "scope": scopeName, "resource": resource}).Add(1)
		}
	}
}
