package libp2pquic

import (
	"bytes"
	"net"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/benbjohnson/clock"

	"github.com/libp2p/go-netroute"
	"github.com/stretchr/testify/require"
)

func (c *reuseConn) GetCount() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.refCount
}

func closeAllConns(reuse *reuse) {
	reuse.mutex.Lock()
	for _, conn := range reuse.global {
		for conn.GetCount() > 0 {
			conn.DecreaseCount()
		}
	}
	for _, conns := range reuse.unicast {
		for _, conn := range conns {
			for conn.GetCount() > 0 {
				conn.DecreaseCount()
			}
		}
	}
	reuse.mutex.Unlock()
}

func platformHasRoutingTables() bool {
	_, err := netroute.New()
	return err == nil
}

func isGarbageCollectorRunning() bool {
	var b bytes.Buffer
	pprof.Lookup("goroutine").WriteTo(&b, 1)
	return strings.Contains(b.String(), "quic.(*reuse).gc")
}

func cleanup(t *testing.T, reuse *reuse) {
	t.Cleanup(func() {
		closeAllConns(reuse)
		reuse.Close()
		require.False(t, isGarbageCollectorRunning(), "reuse gc still running")
	})
}

func TestReuseListenOnAllIPv4(t *testing.T) {
	reuse := newReuse(clock.New())
	require.Eventually(t, isGarbageCollectorRunning, 500*time.Millisecond, 50*time.Millisecond, "expected garbage collector to be running")
	cleanup(t, reuse)

	addr, err := net.ResolveUDPAddr("udp4", "0.0.0.0:0")
	require.NoError(t, err)
	conn, err := reuse.Listen("udp4", addr)
	require.NoError(t, err)
	require.Equal(t, conn.GetCount(), 1)
}

func TestReuseListenOnAllIPv6(t *testing.T) {
	reuse := newReuse(clock.New())
	require.Eventually(t, isGarbageCollectorRunning, 500*time.Millisecond, 50*time.Millisecond, "expected garbage collector to be running")
	cleanup(t, reuse)

	addr, err := net.ResolveUDPAddr("udp6", "[::]:1234")
	require.NoError(t, err)
	conn, err := reuse.Listen("udp6", addr)
	require.NoError(t, err)
	require.Equal(t, conn.GetCount(), 1)
}

func TestReuseCreateNewGlobalConnOnDial(t *testing.T) {
	reuse := newReuse(clock.New())
	cleanup(t, reuse)

	addr, err := net.ResolveUDPAddr("udp4", "1.1.1.1:1234")
	require.NoError(t, err)
	conn, err := reuse.Dial("udp4", addr)
	require.NoError(t, err)
	require.Equal(t, conn.GetCount(), 1)
	laddr := conn.LocalAddr().(*net.UDPAddr)
	require.Equal(t, laddr.IP.String(), "0.0.0.0")
	require.NotEqual(t, laddr.Port, 0)
}

func TestReuseConnectionWhenDialing(t *testing.T) {
	reuse := newReuse(clock.New())
	cleanup(t, reuse)

	addr, err := net.ResolveUDPAddr("udp4", "0.0.0.0:0")
	require.NoError(t, err)
	lconn, err := reuse.Listen("udp4", addr)
	require.NoError(t, err)
	require.Equal(t, lconn.GetCount(), 1)
	// dial
	raddr, err := net.ResolveUDPAddr("udp4", "1.1.1.1:1234")
	require.NoError(t, err)
	conn, err := reuse.Dial("udp4", raddr)
	require.NoError(t, err)
	require.Equal(t, conn.GetCount(), 2)
}

func TestReuseListenOnSpecificInterface(t *testing.T) {
	if platformHasRoutingTables() {
		t.Skip("this test only works on platforms that support routing tables")
	}
	reuse := newReuse(clock.New())
	cleanup(t, reuse)

	router, err := netroute.New()
	require.NoError(t, err)

	raddr, err := net.ResolveUDPAddr("udp4", "1.1.1.1:1234")
	require.NoError(t, err)
	_, _, ip, err := router.Route(raddr.IP)
	require.NoError(t, err)
	// listen
	addr, err := net.ResolveUDPAddr("udp4", ip.String()+":0")
	require.NoError(t, err)
	lconn, err := reuse.Listen("udp4", addr)
	require.NoError(t, err)
	require.Equal(t, lconn.GetCount(), 1)
	// dial
	conn, err := reuse.Dial("udp4", raddr)
	require.NoError(t, err)
	require.Equal(t, conn.GetCount(), 1)
}

func TestReuseGarbageCollect(t *testing.T) {
	cl := clock.NewMock()
	reuse := newReuse(cl)
	cleanup(t, reuse)

	numGlobals := func() int {
		reuse.mutex.Lock()
		defer reuse.mutex.Unlock()
		return len(reuse.global)
	}

	addr, err := net.ResolveUDPAddr("udp4", "0.0.0.0:0")
	require.NoError(t, err)
	lconn, err := reuse.Listen("udp4", addr)
	require.NoError(t, err)
	require.Equal(t, lconn.GetCount(), 1)

	lconn.DecreaseCount()

	require.Equal(t, numGlobals(), 1)
	cl.Add(garbageCollectInterval)
	require.Eventually(t, func() bool { return numGlobals() == 0 }, 200*time.Millisecond, 10*time.Millisecond)
}
