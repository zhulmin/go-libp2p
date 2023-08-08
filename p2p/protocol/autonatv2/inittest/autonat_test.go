package autonat_test

import (
	"testing"

	"github.com/libp2p/go-libp2p"
	basichost "github.com/libp2p/go-libp2p/p2p/host/basic"
	"github.com/stretchr/testify/require"
)

func TestAutoNATInit(t *testing.T) {
	h, err := libp2p.New()
	if err != nil {
		t.Fatal(err)
	}
	bh := h.(*basichost.BasicHost)
	require.NotNil(t, bh.GetAutoNatV2())

	h, err = libp2p.New(libp2p.DisableAutoNATv2())
	if err != nil {
		t.Fatal(err)
	}
	bh = h.(*basichost.BasicHost)
	require.Nil(t, bh.GetAutoNatV2())
}
