package autonatv2

import "time"

type autoNATSettings struct {
	allowAllAddrs     bool
	serverRPM         int
	serverRPMPerPeer  int
	dataRequestPolicy dataRequestPolicyFunc
	now               func() time.Time
}

func defaultSettings() *autoNATSettings {
	return &autoNATSettings{
		allowAllAddrs:     false,
		serverRPM:         20,
		serverRPMPerPeer:  2,
		dataRequestPolicy: defaultDataRequestPolicy,
		now:               time.Now,
	}
}

type AutoNATOption func(s *autoNATSettings) error

func allowAll(s *autoNATSettings) error {
	s.allowAllAddrs = true
	return nil
}

func WithServerRateLimit(rpm, rpmPerPeer int) AutoNATOption {
	return func(s *autoNATSettings) error {
		s.serverRPM = rpm
		s.serverRPMPerPeer = rpmPerPeer
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
