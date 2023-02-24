//go:build nocover

// These tests are in their own package to avoid transitively pulling in other
// deps that may run background tasks in their init and thus allocate. Looking
// at you
// [go-libp2p-asn-util](https://github.com/libp2p/go-libp2p-asn-util/blob/master/asn.go#L14)

package identify_alloc_test

import (
	"math/rand"
	"testing"

	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
)

func TestMetricsNoAllocNoCover(t *testing.T) {
	events := []any{
		event.EvtLocalAddressesUpdated{},
		event.EvtLocalProtocolsUpdated{},
		event.EvtNATDeviceTypeChanged{},
	}

	pushSupport := []identify.IdentifyPushSupport{
		identify.IdentifyPushSupportUnknown,
		identify.IdentifyPushSupported,
		identify.IdentifyPushUnsupported,
	}

	tr := identify.NewMetricsTracer()
	tests := map[string]func(){
		"TriggeredPushes":  func() { tr.TriggeredPushes(events[rand.Intn(len(events))]) },
		"ConnPushSupport":  func() { tr.ConnPushSupport(pushSupport[rand.Intn(len(pushSupport))]) },
		"IdentifyReceived": func() { tr.IdentifyReceived(rand.Intn(2) == 0, rand.Intn(20), rand.Intn(20)) },
		"IdentifySent":     func() { tr.IdentifySent(rand.Intn(2) == 0, rand.Intn(20), rand.Intn(20)) },
	}

	for method, f := range tests {
		allocs := testing.AllocsPerRun(1000, f)
		if allocs > 0 {
			t.Fatalf("Alloc Test: %s, got: %0.2f, expected: 0 allocs", method, allocs)
		}
	}
}
