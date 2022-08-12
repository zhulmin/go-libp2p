package reuseport

import (
	"context"
	"fmt"
	"math/rand"
	"net"

	"github.com/libp2p/go-netroute"
)

type dialer struct {
	specific    []*net.TCPAddr
	loopback    []*net.TCPAddr
	unspecified []*net.TCPAddr
}

func (d *dialer) Dial(network, addr string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, addr)
}

func randAddr(addrs []*net.TCPAddr) *net.TCPAddr {
	if len(addrs) > 0 {
		return addrs[rand.Intn(len(addrs))]
	}
	return nil
}

// DialContext dials a target addr.
// Dialing preference is
// * If there is a listener on the local interface the OS expects to use to route towards addr, use that.
// * If there is a listener on a loopback address, addr is loopback, use that.
// * If there is a listener on an undefined address (0.0.0.0 or ::), use that.
// * Otherwise, dial with a random port on the unspecified address.
func (d *dialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if len(d.specific) > 0 || len(d.loopback) > 0 {
		tcpAddr, err := net.ResolveTCPAddr(network, addr)
		if err != nil {
			return nil, err
		}
		ip := tcpAddr.IP
		if !ip.IsLoopback() && !ip.IsGlobalUnicast() {
			return nil, fmt.Errorf("undialable IP: %s", ip)
		}

		if len(d.specific) > 0 {
			if router, err := netroute.New(); err == nil {
				if _, _, preferredSrc, err := router.Route(ip); err == nil {
					for _, optAddr := range d.specific {
						if optAddr.IP.Equal(preferredSrc) {
							return reuseDial(ctx, optAddr, network, addr)
						}
					}
				}
			}
		}

		if len(d.loopback) > 0 && ip.IsLoopback() {
			return reuseDial(ctx, randAddr(d.loopback), network, addr)
		}
	}

	if len(d.unspecified) > 0 {
		return reuseDial(ctx, randAddr(d.unspecified), network, addr)
	}

	var dialer net.Dialer
	return dialer.DialContext(ctx, network, addr)
}

func newDialer(listeners map[*listener]struct{}) *dialer {
	specific := make([]*net.TCPAddr, 0)
	loopback := make([]*net.TCPAddr, 0)
	unspecified := make([]*net.TCPAddr, 0)

	for l := range listeners {
		addr := l.Addr().(*net.TCPAddr)
		if addr.IP.IsLoopback() {
			loopback = append(loopback, addr)
		} else if addr.IP.IsUnspecified() {
			unspecified = append(unspecified, addr)
		} else {
			specific = append(specific, addr)
		}
	}
	return &dialer{
		specific:    specific,
		loopback:    loopback,
		unspecified: unspecified,
	}
}
