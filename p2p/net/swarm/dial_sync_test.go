package swarm

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p-core/peer"

	"github.com/stretchr/testify/require"
)

func getMockDialFunc() (dialWorkerFunc, func(), context.Context, <-chan struct{}) {
	dfcalls := make(chan struct{}, 512) // buffer it large enough that we won't care
	dialctx, cancel := context.WithCancel(context.Background())
	ch := make(chan struct{})
	f := func(p peer.ID, reqch <-chan dialRequest) {
		defer cancel()
		dfcalls <- struct{}{}
		go func() {
			for req := range reqch {
				<-ch
				req.resch <- dialResponse{conn: new(Conn)}
			}
		}()
	}

	var once sync.Once
	return f, func() { once.Do(func() { close(ch) }) }, dialctx, dfcalls
}

func TestBasicDialSync(t *testing.T) {
	p := peer.ID("testpeer")
	var counter int32
	requests := make(chan dialRequest, 2)
	done := make(chan struct{})
	dsync := newDialSync(func(id peer.ID, reqs <-chan dialRequest) {
		require.Equal(t, id, p)
		atomic.AddInt32(&counter, 1)
		for req := range reqs {
			requests <- req
			go func(req dialRequest) {
				<-done
				req.resch <- dialResponse{conn: new(Conn)}
			}(req)
		}
	})

	finished := make(chan struct{}, 2)
	go func() {
		if _, err := dsync.Dial(context.Background(), p); err != nil {
			t.Error(err)
		}
		finished <- struct{}{}
	}()

	go func() {
		if _, err := dsync.Dial(context.Background(), p); err != nil {
			t.Error(err)
		}
		finished <- struct{}{}
	}()

	// wait until both requests are registered
	require.Eventually(t, func() bool { return len(requests) == 2 }, 100*time.Millisecond, 10*time.Millisecond, "expected both requests to be processed")
	// make the dials return
	close(done)
	// make sure the Dial functions return
	require.Eventually(t, func() bool { return len(finished) == 2 }, 100*time.Millisecond, 10*time.Millisecond, "dial functions should have returned")

	require.Equal(t, 1, int(atomic.LoadInt32(&counter)), "should only have called dial func once!")
}

func TestDialSyncCancel(t *testing.T) {
	df, done, _, dcall := getMockDialFunc()

	dsync := newDialSync(df)

	p := peer.ID("testpeer")

	ctx1, cancel1 := context.WithCancel(context.Background())

	finished := make(chan struct{})
	go func() {
		_, err := dsync.Dial(ctx1, p)
		if err != ctx1.Err() {
			t.Error("should have gotten context error")
		}
		finished <- struct{}{}
	}()

	// make sure the above makes it through the wait code first
	select {
	case <-dcall:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for dial to start")
	}

	// Add a second dialwait in so two actors are waiting on the same dial
	go func() {
		_, err := dsync.Dial(context.Background(), p)
		if err != nil {
			t.Error(err)
		}
		finished <- struct{}{}
	}()

	time.Sleep(time.Millisecond * 20)

	// cancel the first dialwait, it should not affect the second at all
	cancel1()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for wait to exit")
	}

	// short sleep just to make sure we've moved around in the scheduler
	time.Sleep(time.Millisecond * 20)
	done()

	<-finished
}

func TestDialSyncAllCancel(t *testing.T) {
	df, done, dctx, _ := getMockDialFunc()

	dsync := newDialSync(df)
	p := peer.ID("testpeer")
	ctx, cancel := context.WithCancel(context.Background())

	finished := make(chan struct{})
	go func() {
		if _, err := dsync.Dial(ctx, p); err != ctx.Err() {
			t.Error("should have gotten context error")
		}
		finished <- struct{}{}
	}()

	// Add a second dialwait in so two actors are waiting on the same dial
	go func() {
		if _, err := dsync.Dial(ctx, p); err != ctx.Err() {
			t.Error("should have gotten context error")
		}
		finished <- struct{}{}
	}()

	cancel()
	for i := 0; i < 2; i++ {
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for wait to exit")
		}
	}

	// the dial should have exited now
	select {
	case <-dctx.Done():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for dial to return")
	}

	// should be able to successfully dial that peer again
	done()
	if _, err := dsync.Dial(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}

func TestFailFirst(t *testing.T) {
	var count int32
	dialErr := fmt.Errorf("gophers ate the modem")
	f := func(p peer.ID, reqch <-chan dialRequest) {
		go func() {
			for {
				req, ok := <-reqch
				if !ok {
					return
				}

				if atomic.CompareAndSwapInt32(&count, 0, 1) {
					req.resch <- dialResponse{err: dialErr}
				} else {
					req.resch <- dialResponse{conn: new(Conn)}
				}
			}
		}()
	}

	ds := newDialSync(f)
	p := peer.ID("testing")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	_, err := ds.Dial(ctx, p)
	require.ErrorIs(t, err, dialErr, "expected gophers to have eaten the modem")

	c, err := ds.Dial(ctx, p)
	require.NoError(t, err)
	require.NotNil(t, c, "should have gotten a 'real' conn back")
}

func TestStressActiveDial(t *testing.T) {
	ds := newDialSync(func(p peer.ID, reqch <-chan dialRequest) {
		go func() {
			for {
				req, ok := <-reqch
				if !ok {
					return
				}
				req.resch <- dialResponse{}
			}
		}()
	})

	wg := sync.WaitGroup{}

	pid := peer.ID("foo")

	makeDials := func() {
		for i := 0; i < 10000; i++ {
			ds.Dial(context.Background(), pid)
		}
		wg.Done()
	}

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go makeDials()
	}

	wg.Wait()
}
