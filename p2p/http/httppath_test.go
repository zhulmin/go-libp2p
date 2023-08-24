package libp2phttp

import (
	"testing"

	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func TestRoundTrip(t *testing.T) {
	s := "/httppath/foobar%2Fbaz"
	m := ma.StringCast(s)
	v, _ := m.ValueForProtocol(P_HTTPPATH)
	require.Equal(t, "foobar%2Fbaz", v)
	require.Equal(t, s, m.String())
}
