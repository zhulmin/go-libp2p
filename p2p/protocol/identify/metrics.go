package identify

import (
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/prometheus/client_golang/prometheus"
)

const metricNamespace = "libp2p_identify_"

var (
	delta = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    metricNamespace + "identify_delta_bytes",
			Help:    "Identify Delta",
			Buckets: prometheus.LinearBuckets(50, 50, 40),
		},
		[]string{"dir"},
	)
	push = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    metricNamespace + "identify_push_bytes",
			Help:    "Identify Push",
			Buckets: prometheus.LinearBuckets(50, 50, 40),
		},
		[]string{"dir"},
	)
	identify = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    metricNamespace + "identify_bytes",
			Help:    "Identify",
			Buckets: prometheus.LinearBuckets(50, 50, 40),
		},
		[]string{"dir"},
	)
)

func init() {
	prometheus.MustRegister(identify, push, delta)
}

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

func recordDelta(dir network.Direction, size int) {
	delta.WithLabelValues(getDirection(dir)).Observe(float64(size))
}

func recordPush(dir network.Direction, size int) {
	push.WithLabelValues(getDirection(dir)).Observe(float64(size))
}

func recordIdentify(dir network.Direction, size int) {
	identify.WithLabelValues(getDirection(dir)).Observe(float64(size))
}
