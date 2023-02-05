package libp2pwebrtc

import (
	"sync"

	pb "github.com/libp2p/go-libp2p/p2p/transport/webrtc/pb"
)

type (
	channelState struct {
		state channelStateValue
		mu    sync.RWMutex
	}

	channelStateValue uint8
)

func newChannelState() *channelState {
	return &channelState{}
}

const (
	stateReadClosed channelStateValue = 1 << iota
	stateWriteClosed
)

const (
	stateClosed = stateReadClosed | stateWriteClosed
)

func (c *channelState) handleIncomingFlag(flag pb.Message_Flag) (channelStateValue, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == stateClosed {
		return c.state, false
	}

	currentState := c.state
	switch flag {
	case pb.Message_FIN:
		c.state |= stateReadClosed
		return c.state, currentState != c.state
	case pb.Message_STOP_SENDING:
		c.state |= stateWriteClosed
		return c.state, currentState != c.state
	case pb.Message_RESET:
		c.state = stateClosed
		return c.state, currentState != c.state
	default:
		// ignore values that are invalid for flags
		return c.state, false
	}
}

func (c *channelState) processOutgoingFlag(flag pb.Message_Flag) (channelStateValue, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == stateClosed {
		return c.state, false
	}

	currentState := c.state
	switch flag {
	case pb.Message_FIN:
		c.state |= stateWriteClosed
		return c.state, currentState != c.state
	case pb.Message_STOP_SENDING:
		c.state |= stateReadClosed
		return c.state, currentState != c.state
	case pb.Message_RESET:
		c.state = stateClosed
		return c.state, currentState != c.state
	default:
		// ignore values that are invalid for flags
		return c.state, false
	}
}

func (c *channelState) value() channelStateValue {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

func (c *channelState) allowRead() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.allowRead()
}

func (c *channelState) allowWrite() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.allowWrite()
}

func (c *channelState) closed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.closed()
}

func (c *channelState) close() {
	c.mu.Lock()
	c.state = stateClosed
	c.mu.Unlock()
}

func (v channelStateValue) allowRead() bool {
	return v&stateReadClosed == 0
}

func (v channelStateValue) allowWrite() bool {
	return v&stateWriteClosed == 0
}

func (v channelStateValue) closed() bool {
	return v == stateClosed
}
