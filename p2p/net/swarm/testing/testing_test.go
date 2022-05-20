package testing

import (
	"testing"

	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/stretchr/testify/require"
)

func TestGenSwarm(t *testing.T) {
	swarm := GenSwarm(t)
	require.NoError(t, swarm.Close())
	GenUpgrader(t, swarm, yamux.DefaultTransport)
}
