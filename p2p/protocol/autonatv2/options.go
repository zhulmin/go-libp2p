package autonatv2

import "time"

type autoNATSettings struct {
	allowAllAddrs     bool
	serverRPM         int
	serverPerPeerRPM  int
	serverDialDataRPM int
	dataRequestPolicy dataRequestPolicyFunc
	now               func() time.Time
}

func defaultSettings() *autoNATSettings {
	return &autoNATSettings{
		allowAllAddrs: false,
		// TODO: confirm rate limiting defaults
		serverRPM:         20,
		serverPerPeerRPM:  2,
		serverDialDataRPM: 5,
		dataRequestPolicy: amplificationAttackPrevention,
		now:               time.Now,
	}
}

type AutoNATOption func(s *autoNATSettings) error

func allowAll(s *autoNATSettings) error {
	s.allowAllAddrs = true
	return nil
}

func WithServerRateLimit(rpm, perPeerRPM, dialDataRPM int) AutoNATOption {
	return func(s *autoNATSettings) error {
		s.serverRPM = rpm
		s.serverPerPeerRPM = perPeerRPM
		s.serverDialDataRPM = dialDataRPM
		return nil
	}
}

func WithDataRequestPolicy(drp dataRequestPolicyFunc) AutoNATOption {
	return func(s *autoNATSettings) error {
		s.dataRequestPolicy = drp
		return nil
	}
}

func WithNow(now func() time.Time) AutoNATOption {
	return func(s *autoNATSettings) error {
		s.now = now
		return nil
	}
}
