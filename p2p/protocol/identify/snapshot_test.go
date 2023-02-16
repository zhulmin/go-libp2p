package identify

import (
	"crypto/rand"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/core/record"

	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func TestSnapshotEqual(t *testing.T) {
	_, pub, err := crypto.GenerateEd25519Key(rand.Reader)
	require.NoError(t, err)
	addr1 := ma.StringCast("/ip4/127.0.0.1/tcp/1234")
	addr2 := ma.StringCast("/ip4/127.0.0.1/tcp/1235")
	env1 := &record.Envelope{PublicKey: pub, RawPayload: []byte("foo")}
	env2 := &record.Envelope{PublicKey: pub, RawPayload: []byte("bar")}
	require.True(t, identifySnapshot{}.Equal(&identifySnapshot{}))
	require.True(t, identifySnapshot{Sequence: 1}.Equal(&identifySnapshot{Sequence: 2}))
	require.True(t, identifySnapshot{Protocols: []protocol.ID{"a"}}.Equal(&identifySnapshot{Protocols: []protocol.ID{"a"}}))
	require.False(t, identifySnapshot{Protocols: []protocol.ID{"a"}}.Equal(&identifySnapshot{Protocols: []protocol.ID{"b"}}))
	require.False(t, identifySnapshot{}.Equal(&identifySnapshot{Protocols: []protocol.ID{"b"}}))
	require.True(t, identifySnapshot{Addrs: []ma.Multiaddr{addr1}}.Equal(&identifySnapshot{Addrs: []ma.Multiaddr{addr1}}))
	require.False(t, identifySnapshot{Addrs: []ma.Multiaddr{addr1}}.Equal(&identifySnapshot{Addrs: []ma.Multiaddr{addr2}}))
	require.False(t, identifySnapshot{Record: env1}.Equal(&identifySnapshot{}))
	require.False(t, identifySnapshot{Record: env1}.Equal(&identifySnapshot{Record: env2}))
	require.False(t, identifySnapshot{}.Equal(&identifySnapshot{Record: env2}))
	require.True(t, identifySnapshot{Record: env2}.Equal(&identifySnapshot{Record: env2}))
}
