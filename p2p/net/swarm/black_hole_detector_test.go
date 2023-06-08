package swarm

import (
	"testing"

	ma "github.com/multiformats/go-multiaddr"
)

func TestBlackHoleFilterReset(t *testing.T) {
	n := 10
	bhd := &blackHoleFilter{n: n, minSuccessFraction: 0.05, name: "test"}
	var i = 0
	// calls up to threshold should be allowed
	for i = 1; i <= n; i++ {
		if _, isAllowed := bhd.IsAllowed(); !isAllowed {
			t.Fatalf("expected calls up to minDials to be allowed")
		}
		bhd.RecordOutcome(false)
	}
	// after threshold calls every nth call should be allowed
	for i = n + 1; i < 42; i++ {
		_, isAllowed := bhd.IsAllowed()
		if (i%n == 0 && !isAllowed) || (i%n != 0 && isAllowed) {
			t.Fatalf("expected every nth dial to be allowed")
		}
	}

	bhd.RecordOutcome(true)
	// check if calls up to threshold are allowed after success
	for i = 0; i < n; i++ {
		if _, isAllowed := bhd.IsAllowed(); !isAllowed {
			t.Fatalf("expected black hole detector state to reset after success")
		}
		bhd.RecordOutcome(false)
	}

	// next call should be refused
	if _, isAllowed := bhd.IsAllowed(); isAllowed {
		t.Fatalf("expected dial to be blocked")
	}
}

func TestBlackHoleDetector(t *testing.T) {
	n := 10
	bhd := &blackHoleFilter{n: n, minSuccessFraction: 0.4, name: "test"}
	var i = 0
	// 5 success and 5 fails
	for i = 1; i <= 5; i++ {
		bhd.RecordOutcome(true)
	}
	for i = 1; i <= 5; i++ {
		bhd.RecordOutcome(false)
	}

	if _, isAllowed := bhd.IsAllowed(); !isAllowed {
		t.Fatalf("expected dial to be allowed")
	}
	// 4 success and 6 fails
	bhd.RecordOutcome(false)

	if _, isAllowed := bhd.IsAllowed(); !isAllowed {
		t.Fatalf("expected dial to be allowed")
	}
	// 3 success and 7 fails
	bhd.RecordOutcome(false)

	// should be blocked
	if _, isAllowed := bhd.IsAllowed(); isAllowed {
		t.Fatalf("expected dial to be blocked")
	}

	bhd.RecordOutcome(true)
	// 5 success and 5 fails
	for i = 1; i <= 5; i++ {
		bhd.RecordOutcome(true)
	}
	for i = 1; i <= 5; i++ {
		bhd.RecordOutcome(false)
	}

	if _, isAllowed := bhd.IsAllowed(); !isAllowed {
		t.Fatalf("expected dial to be allowed")
	}
	// 4 success and 6 fails
	bhd.RecordOutcome(false)

	if _, isAllowed := bhd.IsAllowed(); !isAllowed {
		t.Fatalf("expected dial to be allowed")
	}
	// 3 success and 7 fails
	bhd.RecordOutcome(false)

	// should be blocked
	if _, isAllowed := bhd.IsAllowed(); isAllowed {
		t.Fatalf("expected dial to be blocked")
	}

}

func TestBlackHoleDetectorInApplicableAddress(t *testing.T) {
	bhd := newBlackHoleDetector(true, true, nil)
	addr := ma.StringCast("/ip4/127.0.0.1/tcp/1234")
	for i := 0; i < 1000; i++ {
		if !bhd.IsAllowed(addr) {
			t.Fatalf("expect dials to inapplicable address to always be allowed")
		}
		bhd.RecordOutcome(addr, false)
	}
}

func TestBlackHoleDetectorUDP(t *testing.T) {
	bhd := newBlackHoleDetector(true, true, nil)
	addr := ma.StringCast("/ip4/1.2.3.4/udp/1234")
	for i := 0; i < 100; i++ {
		bhd.RecordOutcome(addr, false)
	}
	if bhd.IsAllowed(addr) {
		t.Fatalf("expect dial to be be blocked")
	}

	bhd = newBlackHoleDetector(false, true, nil)
	for i := 0; i < 100; i++ {
		bhd.RecordOutcome(addr, false)
	}
	if !bhd.IsAllowed(addr) {
		t.Fatalf("expected dial to be be allowed when UDP detection is disabled")
	}
}

func TestBlackHoleDetectorIPv6(t *testing.T) {
	bhd := newBlackHoleDetector(true, true, nil)
	addr := ma.StringCast("/ip6/1::1/tcp/1234")
	for i := 0; i < 100; i++ {
		bhd.RecordOutcome(addr, false)
	}
	if bhd.IsAllowed(addr) {
		t.Fatalf("expect dial to be be blocked")
	}

	bhd = newBlackHoleDetector(true, false, nil)
	for i := 0; i < 100; i++ {
		bhd.RecordOutcome(addr, false)
	}
	if !bhd.IsAllowed(addr) {
		t.Fatalf("expected dial to be be allowed when IPv6 detection is disabled")
	}
}

func TestBlackHoleDetectorProbes(t *testing.T) {
	bhd := &blackHoleDetector{
		udp:  &blackHoleFilter{n: 2, minSuccessFraction: 0.5},
		ipv6: &blackHoleFilter{n: 3, minSuccessFraction: 0.5},
	}
	udp6Addr := ma.StringCast("/ip6/1::1/udp/1234/quic-v1")
	for i := 0; i < 3; i++ {
		bhd.RecordOutcome(udp6Addr, false)
	}
	for i := 1; i < 100; i++ {
		isAllowed := bhd.IsAllowed(udp6Addr)
		if i%2 == 0 || i%3 == 0 {
			if !isAllowed {
				t.Fatalf("expected probe to be allowed irrespective of the state of other black hole filter")
			}
		} else {
			if isAllowed {
				t.Fatalf("expected dial to be blocked")
			}
		}
	}

}
