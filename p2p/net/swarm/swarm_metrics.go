package swarm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"

	ma "github.com/multiformats/go-multiaddr"

	"github.com/prometheus/client_golang/prometheus"
)

const metricNamespace = "libp2p_swarm_"

var (
	connsOpened = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: metricNamespace + "connections_opened_total",
			Help: "Connections Opened",
		},
		[]string{"dir", "transport", "security", "muxer"},
	)
	keyTypes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: metricNamespace + "key_types_total",
			Help: "key type",
		},
		[]string{"dir", "key_type"},
	)
	connsClosed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: metricNamespace + "connections_closed_total",
			Help: "Connections Closed",
		},
		[]string{"dir", "transport", "security", "muxer"},
	)
	dialError = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: metricNamespace + "dial_errors_total",
			Help: "Dial Error",
		},
		[]string{"error"},
	)
	connDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    metricNamespace + "connection_duration_seconds",
			Help:    "Duration of a Connection",
			Buckets: prometheus.ExponentialBuckets(1.0/16, 2, 25), // up to 24 days
		},
		[]string{"dir", "transport", "security", "muxer"},
	)
	connHandshakeLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    metricNamespace + "handshake_latency_seconds",
			Help:    "Duration of the libp2p Handshake",
			Buckets: prometheus.ExponentialBuckets(0.001, 1.3, 35),
		},
		[]string{"transport", "security", "muxer"},
	)
)

func init() {
	prometheus.MustRegister(connsOpened, keyTypes, connsClosed, dialError, connDuration, connHandshakeLatency)
}

var transports = [...]int{ma.P_CIRCUIT, ma.P_WEBRTC, ma.P_WEBTRANSPORT, ma.P_QUIC, ma.P_QUIC_V1, ma.P_WSS, ma.P_WS, ma.P_TCP}

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

func appendConnectionState(tags []string, cs network.ConnectionState) []string {
	if cs.Transport == "" {
		// This shouldn't happen, unless the transport doesn't properly set the Transport field in the ConnectionState.
		tags = append(tags, "unknown")
	} else {
		tags = append(tags, cs.Transport)
	}
	// These might be empty, depending on the transport.
	// For example, QUIC doesn't set security nor muxer.
	tags = append(tags, cs.Security)
	tags = append(tags, cs.StreamMultiplexer)
	return tags
}

func recordConnectionOpened(dir network.Direction, p crypto.PubKey, cs network.ConnectionState) {
	tags := make([]string, 0, 4)
	tags = append(tags, getDirection(dir))
	tags = appendConnectionState(tags, cs)
	connsOpened.WithLabelValues(tags...).Inc()
	keyTypes.WithLabelValues(getDirection(dir), p.Type().String()).Inc()
}

func recordConnectionClosed(dir network.Direction, cs network.ConnectionState) {
	tags := make([]string, 0, 4)
	tags = append(tags, getDirection(dir))
	tags = appendConnectionState(tags, cs)
	connsClosed.WithLabelValues(tags...).Inc()
}

func recordConnectionDuration(dir network.Direction, t time.Duration, cs network.ConnectionState) {
	tags := make([]string, 0, 4)
	tags = append(tags, getDirection(dir))
	tags = appendConnectionState(tags, cs)
	connDuration.WithLabelValues(tags...).Observe(t.Seconds())
}

func recordHandshakeLatency(t time.Duration, cs network.ConnectionState) {
	tags := make([]string, 0, 3)
	tags = appendConnectionState(tags, cs)
	connHandshakeLatency.WithLabelValues(tags...).Observe(t.Seconds())
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
	dialError.WithLabelValues(e).Inc()
}
