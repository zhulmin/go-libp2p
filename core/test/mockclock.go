package test

import (
	"sort"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
)

type mockClock struct {
	mu           sync.Mutex
	now          time.Time
	timers       []*mockInstantTimer
	advanceBySem chan struct{}
}

type mockInstantTimer struct {
	c      *mockClock
	mu     sync.Mutex
	when   time.Time
	active bool
	ch     chan time.Time
}

func (t *mockInstantTimer) Ch() <-chan time.Time {
	return t.ch
}

func (t *mockInstantTimer) Reset(d time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	wasActive := t.active
	t.active = true
	t.when = d

	// Schedule any timers that need to run. This will run this timer if t.when is before c.now
	go t.c.AdvanceBy(0)

	return wasActive
}

func (t *mockInstantTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	wasActive := t.active
	t.active = false
	return wasActive
}

var _ autorelay.InstantTimer = &mockInstantTimer{}
var _ autorelay.ClockWithInstantTimer = &mockClock{}

func NewMockClock() *mockClock {
	return &mockClock{now: time.Unix(0, 0), advanceBySem: make(chan struct{}, 1)}
}

// InstantTimer implements autorelay.ClockWithInstantTimer
func (c *mockClock) InstantTimer(when time.Time) autorelay.InstantTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &mockInstantTimer{
		c:      c,
		when:   when,
		ch:     make(chan time.Time, 1),
		active: true,
	}
	c.timers = append(c.timers, t)
	return t
}

// Since implements autorelay.ClockWithInstantTimer
func (c *mockClock) Since(t time.Time) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now.Sub(t)
}

func (c *mockClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *mockClock) AdvanceBy(dur time.Duration) {
	c.advanceBySem <- struct{}{}
	defer func() { <-c.advanceBySem }()

	c.mu.Lock()
	now := c.now
	endTime := c.now.Add(dur)
	c.mu.Unlock()

	// sort timers by when
	if len(c.timers) > 1 {
		sort.Slice(c.timers, func(i, j int) bool {
			c.timers[i].mu.Lock()
			c.timers[j].mu.Lock()
			defer c.timers[i].mu.Unlock()
			defer c.timers[j].mu.Unlock()
			return c.timers[i].when.Before(c.timers[j].when)
		})
	}

	for _, t := range c.timers {
		t.mu.Lock()
		if !t.active {
			t.mu.Unlock()
			continue
		}
		if !t.when.After(now) {
			t.active = false
			t.mu.Unlock()
			// This may block if the channel is full, but that's intended. This way our mock clock never gets too far ahead of consumer.
			// This also prevents us from dropping times because we're advancing too fast.
			t.ch <- now
		} else if !t.when.After(endTime) {
			now = t.when
			c.mu.Lock()
			c.now = now
			c.mu.Unlock()

			t.active = false
			t.mu.Unlock()
			// This may block if the channel is full, but that's intended. See comment above
			t.ch <- c.now
		} else {
			t.mu.Unlock()
		}
	}
	c.mu.Lock()
	c.now = endTime
	c.mu.Unlock()
}
