package libp2pwebrtc

import "crypto/rand"

const (
	uFragAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890"
	uFragPrefix   = "libp2p+webrtc+v1/"
	uFragIdLength = 32
	uFragIdOffset = len(uFragPrefix)
	uFragLength   = uFragIdOffset + uFragIdLength
)

func genUfrag(n int) string {
	b := make([]byte, uFragLength)
	copy(b[:], uFragPrefix[:])
	rand.Read(b[uFragIdOffset:])
	for i := uFragIdOffset; i < uFragLength; i++ {
		b[i] = uFragAlphabet[int(b[i])%len(uFragAlphabet)]
	}
	return string(b)
}
