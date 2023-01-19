package rcmgr

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/stretchr/testify/require"
)

func withMemoryLimit(l BaseLimit, m int64) BaseLimit {
	l2 := l
	l2.Memory = m
	return l2
}

func TestLimitConfigParserBackwardsCompat(t *testing.T) {
	in, err := os.Open("limit_config_test.json")
	require.NoError(t, err)
	defer in.Close()

	DefaultLimits.AddServiceLimit("C", DefaultLimits.ServiceBaseLimit, BaseLimitIncrease{})
	DefaultLimits.AddProtocolPeerLimit("C", DefaultLimits.ServiceBaseLimit, BaseLimitIncrease{})
	defaults := DefaultLimits.AutoScale()
	cfg, err := readLimiterConfigFromJSON(in, defaults)
	require.NoError(t, err)

	require.Equal(t, int64(65536), cfg.system.Memory)
	require.Equal(t, defaults.system.Streams, cfg.system.Streams)
	require.Equal(t, defaults.system.StreamsInbound, cfg.system.StreamsInbound)
	require.Equal(t, defaults.system.StreamsOutbound, cfg.system.StreamsOutbound)
	require.Equal(t, 16, cfg.system.Conns)
	require.Equal(t, 8, cfg.system.ConnsInbound)
	require.Equal(t, 16, cfg.system.ConnsOutbound)
	require.Equal(t, 16, cfg.system.FD)

	require.Equal(t, defaults.transient, cfg.transient)
	require.Equal(t, int64(8765), cfg.serviceDefault.Memory)

	require.Contains(t, cfg.service, "A")
	require.Equal(t, withMemoryLimit(cfg.serviceDefault, 8192), cfg.service["A"])
	require.Contains(t, cfg.service, "B")
	require.Equal(t, cfg.serviceDefault, cfg.service["B"])
	require.Contains(t, cfg.service, "C")
	require.Equal(t, defaults.service["C"], cfg.service["C"])

	require.Equal(t, int64(4096), cfg.peerDefault.Memory)
	peerID, err := peer.Decode("12D3KooWPFH2Bx2tPfw6RLxN8k2wh47GRXgkt9yrAHU37zFwHWzS")
	require.NoError(t, err)
	require.Contains(t, cfg.peer, peerID)
	require.Equal(t, int64(4097), cfg.peer[peerID].Memory)
}

func TestLimitConfigParser(t *testing.T) {
	in, err := os.Open("limit_config_test.json")
	require.NoError(t, err)
	defer in.Close()

	DefaultLimits.AddServiceLimit("C", DefaultLimits.ServiceBaseLimit, BaseLimitIncrease{})
	DefaultLimits.AddProtocolPeerLimit("C", DefaultLimits.ServiceBaseLimit, BaseLimitIncrease{})
	defaults := DefaultLimits.AutoScale()
	cfg, err := readLimiterConfigFromJSON(in, defaults)
	require.NoError(t, err)

	require.Equal(t, int64(65536), cfg.system.Memory)
	require.Equal(t, defaults.system.Streams, cfg.system.Streams)
	require.Equal(t, defaults.system.StreamsInbound, cfg.system.StreamsInbound)
	require.Equal(t, defaults.system.StreamsOutbound, cfg.system.StreamsOutbound)
	require.Equal(t, 16, cfg.system.Conns)
	require.Equal(t, 8, cfg.system.ConnsInbound)
	require.Equal(t, 16, cfg.system.ConnsOutbound)
	require.Equal(t, 16, cfg.system.FD)

	require.Equal(t, defaults.transient, cfg.transient)
	require.Equal(t, int64(8765), cfg.serviceDefault.Memory)

	require.Contains(t, cfg.service, "A")
	require.Equal(t, withMemoryLimit(cfg.serviceDefault, 8192), cfg.service["A"])
	require.Contains(t, cfg.service, "B")
	require.Equal(t, cfg.serviceDefault, cfg.service["B"])
	require.Contains(t, cfg.service, "C")
	require.Equal(t, defaults.service["C"], cfg.service["C"])

	require.Equal(t, int64(4096), cfg.peerDefault.Memory)
	peerID, err := peer.Decode("12D3KooWPFH2Bx2tPfw6RLxN8k2wh47GRXgkt9yrAHU37zFwHWzS")
	require.NoError(t, err)
	require.Contains(t, cfg.peer, peerID)
	require.Equal(t, int64(4097), cfg.peer[peerID].Memory)

	// Roundtrip
	limitConfig := FromReifiedLimitConfig(cfg, defaults)
	jsonBytes, err := json.Marshal(&limitConfig)
	require.NoError(t, err)
	cfgAfterRoundTrip, err := readLimiterConfigFromJSON(bytes.NewReader(jsonBytes), defaults)
	require.NoError(t, err)
	require.Equal(t, cfg, cfgAfterRoundTrip)
}
