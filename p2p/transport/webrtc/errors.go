package libp2pwebrtc

import (
	"fmt"
)

var (
	errDataChannelTimeout = errDatachannel("timed out waiting for datachannel", nil)
)

func errConnectionFailed(msg string, err error) error {
	return fmt.Errorf("peerconnection failed: %s: %v", msg, err)
}

func errDatachannel(msg string, err error) error {
	return fmt.Errorf("datachannel error: %s: %v", msg, err)
}

func errMultiaddr(msg string, err error) error {
	return fmt.Errorf("bad multiaddr: %s: %v", msg, err)
}

func errNoise(msg string, err error) error {
	return fmt.Errorf("webrtc noise error: %s: %v", msg, err)
}

func errInternal(msg string, err error) error {
	return fmt.Errorf("webrtc internal error: %s: %v", msg, err)
}
