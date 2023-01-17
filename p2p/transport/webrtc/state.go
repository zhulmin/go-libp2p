package libp2pwebrtc

import (
	pb "github.com/libp2p/go-libp2p/p2p/transport/webrtc/pb"
)

type channelState uint8

const (
	stateOpen channelState = 0

	stateReadClosed channelState = 1 << iota
	stateWriteClosed

	stateClosed = stateReadClosed | stateWriteClosed
)

func (state channelState) handleIncomingFlag(flag pb.Message_Flag) channelState {
	if state == stateClosed {
		return state
	}
	switch flag {
	case pb.Message_FIN:
		return state | stateReadClosed

	case pb.Message_STOP_SENDING:
		return state | stateWriteClosed
	case pb.Message_RESET:
		return stateClosed
	default:
		// ignore values that are invalid for flags
		return state
	}
}

func (state channelState) processOutgoingFlag(flag pb.Message_Flag) channelState {
	if state == stateClosed {
		return state
	}

	switch flag {
	case pb.Message_FIN:
		return state | stateWriteClosed
	case pb.Message_STOP_SENDING:
		return state | stateReadClosed
	case pb.Message_RESET:
		return stateClosed
	default:
		// ignore values that are invalid for flags
		return state
	}
}

func (state channelState) allowRead() bool {
	return state&stateReadClosed == 0
}

func (state channelState) allowWrite() bool {
	return state&stateWriteClosed == 0
}
