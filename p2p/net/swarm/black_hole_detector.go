package swarm

import (
	"sync"

	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type blackHoleState int

const (
	blackHoleStateAllowed blackHoleState = iota
	blackHoleStateBlocked
)

type blackHoleResult int

const (
	blackHoleResultAllowed blackHoleResult = iota
	blackHoleResultProbing
	blackHoleResultBlocked
)

// blackHoleFilter provides black hole filtering logic for dials. On detecting a black holed
// network environment, subsequent dials are blocked and only 1 dial every n requests is allowed.
// This should be used in conjunction with an UDP or IPv6 address filter to detect UDP or
// IPv6 black hole.
// Requests are blocked if the success fraction in the last n outcomes is less than
// minSuccessFraction. If a request succeeds in Blocked state, the filter state is reset and n
// subsequent requests are allowed before reevaluating black hole status. Evaluating over n
// outcomes avoids situations where a dial was cancelled because a competing dial succeeded,
// the address was unreachable, and other false negatives.
type blackHoleFilter struct {
	// n is the minimum number of completed dials required before we start blocking.
	// Every nth request is allowed irrespective of the status of the detector.
	n int
	// minSuccessFraction is the minimum success fraction required to allow dials.
	minSuccessFraction float64
	// name for the detector.
	name string

	// requests counts number of dial requests up to n. Resets to 0 every nth request.
	requests int
	// dialResults of the last `n` allowed dials. success is true.
	dialResults []bool
	// successes is the count of successful dials in outcomes
	successes int
	// failures is the count of failed dials in outcomes
	failures int
	// state is the current state of the detector
	state blackHoleState

	mu            sync.Mutex
	metricsTracer MetricsTracer
}

// RecordResult records the outcome of a dial. A successful dial will change the state
// of the filter to Allowed. A failed dial only blocks subsequent requests if the success
// fraction over the last n outcomes is less than the minSuccessFraction of the filter.
func (b *blackHoleFilter) RecordResult(success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == blackHoleStateBlocked && success {
		// If the call succeeds in a blocked state we reset to allowed.
		// This is better than slowly accumulating values till we cross the minSuccessFraction
		// threshold since a blackhole is a binary property.
		b.reset()
		return
	}

	if success {
		b.successes++
	} else {
		b.failures++
	}
	b.dialResults = append(b.dialResults, success)

	if len(b.dialResults) > b.n {
		if b.dialResults[0] {
			b.successes--
		} else {
			b.failures--
		}
		b.dialResults = b.dialResults[1 : b.n+1]
	}

	b.updateState()
	b.trackMetrics()
}

// HandleRequest handles a new dial request for the filter. It returns a
func (b *blackHoleFilter) HandleRequest() blackHoleResult {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.requests++

	b.trackMetrics()

	if b.state == blackHoleStateAllowed {
		return blackHoleResultAllowed
	} else if b.requests%b.n == 0 {
		return blackHoleResultProbing
	} else {
		return blackHoleResultBlocked
	}
}

func (b *blackHoleFilter) reset() {
	b.successes = 0
	b.failures = 0
	b.dialResults = b.dialResults[:0]
	b.requests = 0
	b.updateState()
}

func (b *blackHoleFilter) updateState() {
	st := b.state
	successFraction := 0.0
	if len(b.dialResults) < b.n {
		b.state = blackHoleStateAllowed
	} else {
		successFraction = float64(b.successes) / float64(b.successes+b.failures)
		if successFraction >= b.minSuccessFraction {
			b.state = blackHoleStateAllowed
		} else {
			b.state = blackHoleStateBlocked
		}
	}
	if st != b.state {
		if b.state == blackHoleStateAllowed {
			log.Debugf("%s blackHoleDetector state changed to Allowed", b.name)
		} else {
			log.Debugf("%s blackHoleDetector state changed to Blocked. Success fraction is %0.3f", b.name, successFraction)
		}
	}
}

func (b *blackHoleFilter) trackMetrics() {
	if b.metricsTracer == nil {
		return
	}
	successFraction := 0.0
	if b.successes+b.failures != 0 {
		successFraction = float64(b.successes) / float64(b.successes+b.failures)
	}

	nextRequestAllowedAfter := 0
	if b.state == blackHoleStateBlocked {
		nextRequestAllowedAfter = b.n - (b.requests % b.n)
	}
	b.metricsTracer.UpdatedBlackHoleFilterState(
		b.name,
		b.state,
		nextRequestAllowedAfter,
		successFraction,
	)
}

// blackHoleDetector provides UDP and IPv6 black hole detection using a `blackHoleFilter`
// for each. For details of the black hole detection logic see `blackHoleFilter`
type blackHoleDetector struct {
	udp, ipv6 *blackHoleFilter
}

func (d *blackHoleDetector) HandleRequest(addr ma.Multiaddr) bool {
	if !manet.IsPublicAddr(addr) {
		return true
	}

	udpres := blackHoleResultAllowed
	if d.udp != nil && isProtocolAddr(addr, ma.P_UDP) {
		udpres = d.udp.HandleRequest()
	}

	ipv6res := blackHoleResultAllowed
	if d.ipv6 != nil && isProtocolAddr(addr, ma.P_IP6) {
		ipv6res = d.ipv6.HandleRequest()
	}

	// Allow all probes irrespective of the state of the other filter
	if udpres == blackHoleResultProbing || ipv6res == blackHoleResultProbing {
		return true
	}
	return udpres != blackHoleResultBlocked && ipv6res != blackHoleResultBlocked
}

// RecordResult updates the state of the relevant `blackHoleFilter` for addr
func (d *blackHoleDetector) RecordResult(addr ma.Multiaddr, success bool) {
	if !manet.IsPublicAddr(addr) {
		return
	}
	if d.udp != nil && isProtocolAddr(addr, ma.P_UDP) {
		d.udp.RecordResult(success)
	}
	if d.ipv6 != nil && isProtocolAddr(addr, ma.P_IP6) {
		d.ipv6.RecordResult(success)
	}
}

func newBlackHoleDetector(detectUDP, detectIPv6 bool, mt MetricsTracer) *blackHoleDetector {
	d := &blackHoleDetector{}

	// A black hole is a binary property. On a network if UDP dials are blocked or there is
	// no IPv6 connectivity, all dials will fail. So a low min success fraction like 0.01 is
	// good enough.
	if detectUDP {
		d.udp = &blackHoleFilter{n: 100, minSuccessFraction: 0.01, name: "UDP", metricsTracer: mt}
	}
	if detectIPv6 {
		d.ipv6 = &blackHoleFilter{n: 100, minSuccessFraction: 0.01, name: "IPv6", metricsTracer: mt}
	}
	return d
}
