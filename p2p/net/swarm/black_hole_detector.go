package swarm

import (
	"sync"

	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type outcome int

const (
	outcomeSuccess outcome = iota
	outcomeFailed
)

type blackHoleState int

const (
	blackHoleStateAllowed blackHoleState = iota
	blackHoleStateBlocked
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
	// outcomes of the last `n` allowed dials
	outcomes []outcome
	// successes is the count of successful dials in outcomes
	successes int
	// failures is the count of failed dials in outcomes
	failures int
	// state is the current state of the detector
	state blackHoleState

	mu            sync.Mutex
	metricsTracer MetricsTracer
}

// RecordOutcome records the outcome of a dial. A successful dial will change the state
// of the filter to Allowed. A failed dial only blocks subsequent requests if the success
// fraction over the last n outcomes is less than the minSuccessFraction of the filter.
func (b *blackHoleFilter) RecordOutcome(success bool) {
	if b == nil {
		return
	}

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
		b.outcomes = append(b.outcomes, outcomeSuccess)
	} else {
		b.failures++
		b.outcomes = append(b.outcomes, outcomeFailed)
	}

	if len(b.outcomes) > b.n {
		if b.outcomes[0] == outcomeSuccess {
			b.successes--
		} else {
			b.failures--
		}
		b.outcomes = b.outcomes[1 : b.n+1]
	}

	b.updateState()
	b.trackMetrics()
}

func (b *blackHoleFilter) IsAllowed() (state blackHoleState, isAllowed bool) {
	if b == nil {
		return blackHoleStateAllowed, true
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.requests++
	if b.requests == b.n {
		b.requests = 0
	}
	b.trackMetrics()
	return b.state, (b.state == blackHoleStateAllowed) || (b.requests == 0)
}

func (b *blackHoleFilter) reset() {
	b.successes = 0
	b.failures = 0
	b.outcomes = b.outcomes[:0]
	b.updateState()
}

func (b *blackHoleFilter) updateState() {
	st := b.state
	successFraction := 0.0
	if len(b.outcomes) < b.n {
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
	b.metricsTracer.UpdatedBlackHoleFilterState(
		b.name,
		b.state,
		b.n-b.requests,
		successFraction,
	)
}

// blackHoleDetector provides UDP and IPv6 black hole detection using a `blackHoleFilter`
// for each. For details of the black hole detection logic see `blackHoleFilter`
type blackHoleDetector struct {
	udp, ipv6 *blackHoleFilter
}

func (d *blackHoleDetector) IsAllowed(addr ma.Multiaddr) bool {
	if !manet.IsPublicAddr(addr) {
		return true
	}

	udpState, udpAllowed := blackHoleStateAllowed, true
	if d.udp != nil && isProtocolAddr(addr, ma.P_UDP) {
		udpState, udpAllowed = d.udp.IsAllowed()
	}

	ipv6State, ipv6Allowed := blackHoleStateAllowed, true
	if d.ipv6 != nil && isProtocolAddr(addr, ma.P_IP6) {
		ipv6State, ipv6Allowed = d.ipv6.IsAllowed()
	}

	// Allow all probes irrespective of the state of the other filter
	if (udpState == blackHoleStateBlocked && udpAllowed) ||
		(ipv6State == blackHoleStateBlocked && ipv6Allowed) {
		return true
	}
	return (udpAllowed && ipv6Allowed)
}

// RecordOutcome updates the state of the relevant `blackHoleFilter` for addr
func (d *blackHoleDetector) RecordOutcome(addr ma.Multiaddr, success bool) {
	if !manet.IsPublicAddr(addr) {
		return
	}
	if d.udp != nil && isProtocolAddr(addr, ma.P_UDP) {
		d.udp.RecordOutcome(success)
	}
	if d.ipv6 != nil && isProtocolAddr(addr, ma.P_IP6) {
		d.ipv6.RecordOutcome(success)
	}
}

func newBlackHoleDetector(detectUDP, detectIPv6 bool, mt MetricsTracer) *blackHoleDetector {
	d := &blackHoleDetector{}
	if detectUDP {
		d.udp = &blackHoleFilter{n: 100, minSuccessFraction: 0.01, name: "UDP", metricsTracer: mt}
	}
	if detectIPv6 {
		d.ipv6 = &blackHoleFilter{n: 100, minSuccessFraction: 0.01, name: "IPv6", metricsTracer: mt}
	}
	return d
}
