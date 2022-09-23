package libp2pquic

import "github.com/benbjohnson/clock"

type Option func(opts *config) error

type config struct {
	disableReuseport bool
	metrics          bool
	clock            clock.Clock
}

func (cfg *config) apply(opts ...Option) error {
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return err
		}
	}
	if cfg.clock == nil {
		cfg.clock = clock.New()
	}
	return nil
}

func DisableReuseport() Option {
	return func(cfg *config) error {
		cfg.disableReuseport = true
		return nil
	}
}

// WithMetrics enables Prometheus metrics collection.
func WithMetrics() Option {
	return func(cfg *config) error {
		cfg.metrics = true
		return nil
	}
}

// WithClock sets a clock.
// Probably only useful for testing.
func WithClock(cl clock.Clock) Option {
	return func(cfg *config) error {
		cfg.clock = cl
		return nil
	}
}
