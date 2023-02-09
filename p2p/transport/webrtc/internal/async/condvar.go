package async

import "sync"

// CondVar is an edge-Signaled condition shared by multiple goroutines.  It is
// analogous in effect to the standard condition variable (sync.Cond) but uses
// a channel for signaling.
//
// To wait on the CondVar, call Wait to obtain a channel. This channel will be
// closed the next time the CondVar method is executed. A fresh channel is
// created after each call to CondVar, and is shared among all calls to Wait
// that occur in the window between signals.
//
// A zero CondVar is ready for use but must not be copied after first use.
type CondVar struct {
	sync.Mutex
	ch chan struct{}

	// The signal channel is lazily initialized by the first waiter.
}

// Signal wakes all pending waiters and resets the CondVar.
func (cv *CondVar) Signal() {
	cv.Lock()
	defer cv.Unlock()

	// If the channel is nil, there are no goroutines waiting.  The next waiter
	// will update the channel (see Wait).
	if cv.ch != nil {
		close(cv.ch)
		cv.ch = nil
	}
}

// Wait returns a channel that is closed when cv is signaled.
func (cv *CondVar) Wait() <-chan struct{} {
	cv.Lock()
	defer cv.Unlock()

	// The first waiter after construction or a signal lazily populates the
	// channel for the next period.
	if cv.ch == nil {
		cv.ch = make(chan struct{})
	}
	return cv.ch
}
