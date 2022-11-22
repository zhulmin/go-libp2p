package libp2pwebrtc

import (
	pb "github.com/libp2p/go-libp2p/p2p/transport/webrtc/pb"
)

type channelState uint32

const (
	stateOpen channelState = iota
	stateReadClosed
	stateWriteClosed
	stateClosed
)

func (state channelState) handleIncomingFlag(flag pb.Message_Flag) channelState {
	if state == stateClosed {
		return state
	}
	switch flag {
	case pb.Message_FIN:
		if state == stateWriteClosed {
			return stateClosed
		}
		return stateReadClosed

	case pb.Message_STOP_SENDING:
		if state == stateReadClosed {
			return stateClosed
		}
		return stateWriteClosed
	case pb.Message_RESET:
		return stateClosed

	}
	return state
}

func (state channelState) processOutgoingFlag(flag pb.Message_Flag) channelState {
	if state == stateClosed {
		return state
	}

	switch flag {
	case pb.Message_FIN:
		if state == stateReadClosed {
			return stateClosed
		}
		return stateWriteClosed
	case pb.Message_STOP_SENDING:
		if state == stateWriteClosed {
			return stateClosed
		}
		return stateReadClosed
	case pb.Message_RESET:
		return stateClosed
	}
	return state
}
