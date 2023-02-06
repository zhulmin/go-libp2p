package internal

import (
	"sync"

	pb "github.com/libp2p/go-libp2p/p2p/transport/webrtc/pb"
)

type (
	ChannelState struct {
		state ChannelStateValue
		mu    sync.RWMutex
	}

	ChannelStateValue uint8
)

func NewChannelState() *ChannelState {
	return &ChannelState{}
}

const (
	stateReadClosed ChannelStateValue = 1 << iota
	stateWriteClosed
)

const (
	stateClosed = stateReadClosed | stateWriteClosed
)

func (c *ChannelState) HandleIncomingFlag(flag pb.Message_Flag) (ChannelStateValue, bool) {
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

func (c *ChannelState) ProcessOutgoingFlag(flag pb.Message_Flag) (ChannelStateValue, bool) {
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

func (c *ChannelState) Value() ChannelStateValue {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

func (c *ChannelState) AllowRead() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.AllowRead()
}

func (c *ChannelState) AllowWrite() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.AllowWrite()
}

func (c *ChannelState) Closed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.Closed()
}

func (c *ChannelState) Close() {
	c.mu.Lock()
	c.state = stateClosed
	c.mu.Unlock()
}

func (v ChannelStateValue) AllowRead() bool {
	return v&stateReadClosed == 0
}

func (v ChannelStateValue) AllowWrite() bool {
	return v&stateWriteClosed == 0
}

func (v ChannelStateValue) Closed() bool {
	return v == stateClosed
}
