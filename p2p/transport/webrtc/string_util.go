package libp2pwebrtc

import "unsafe"

func intersperse2(s string, c byte, k int) string {
	m := len(s)
	n := 0
	buf := make([]byte, m+m/k)
	for i, b := range []byte(s) {
		if i%k == 0 && i > 0 {
			buf[n] = c
			n++
		}
		buf[n] = b
		n++
	}
	buf = buf[:n]
	return *(*string)(unsafe.Pointer(&buf))
}
