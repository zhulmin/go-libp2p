package libp2pwebrtc

import (
	"bufio"
	"context"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-msgio/protoio"
	"github.com/pion/datachannel"
	"github.com/pion/webrtc/v3"

	pb "github.com/libp2p/go-libp2p/p2p/transport/webrtc/pb"
)

var _ network.MuxedStream = &dataChannel{}

const (
	// maxMessageSize is limited to 16384 bytes in the SDP.
	maxMessageSize int = 16384
	// maxMessageSize is set to 1MB since pion SCTP streams have
	// an internal buffer size of 1MB by default. Currently, there is
	// no method to change this value when creating a datachannel
	// or a PeerConnection.
	// https://github.com/pion/sctp/blob/c0159aa2d49c240362038edf88baa8a9e6cfcede/association.go#L47
	maxBufferedAmount int = 1024 * 1024
	// bufferedAmountLowThreshold and maxBufferedAmount are bound
	// to a stream but congestion control is done on the whole
	// SCTP association. This means that a single stream can monopolize
	// the complete congestion control window (cwnd) if it does not
	// read stream data and it's remote continues to send. We can
	// add messages to the send buffer once there is space for 1 full
	// sized message.
	bufferedAmountLowThreshold uint64 = uint64(maxMessageSize)

	protoOverhead  int = 5
	varintOverhead int = 2
)

// Package pion detached data channel into a net.Conn
// and then a network.MuxedStream
type dataChannel struct {
	channel *webrtc.DataChannel
	rwc     datachannel.ReadWriteCloser
	laddr   net.Addr
	raddr   net.Addr

	closeWriteOnce sync.Once
	closeReadOnce  sync.Once
	resetOnce      sync.Once

	state channelState

	ctx        context.Context
	cancelFunc context.CancelFunc
	reader     protoio.Reader
	writer     protoio.Writer

	requestRead     chan struct{}
	receivedMessage chan struct{}

	m               sync.Mutex
	readBuf         []byte
	readDeadline    time.Time
	writeDeadline   time.Time
	writeAvailable  chan struct{}
	deadlineUpdated chan struct{}

	wg sync.WaitGroup
}

func newDataChannel(
	channel *webrtc.DataChannel,
	rwc datachannel.ReadWriteCloser,
	pc *webrtc.PeerConnection,
	laddr, raddr net.Addr) *dataChannel {
	ctx, cancel := context.WithCancel(context.Background())

	reader := bufio.NewReaderSize(rwc, maxMessageSize)

	result := &dataChannel{
		channel:         channel,
		rwc:             rwc,
		laddr:           laddr,
		raddr:           raddr,
		ctx:             ctx,
		cancelFunc:      cancel,
		writeAvailable:  make(chan struct{}),
		reader:          protoio.NewDelimitedReader(reader, maxMessageSize),
		writer:          protoio.NewDelimitedWriter(rwc),
		requestRead:     make(chan struct{}, 1),
		receivedMessage: make(chan struct{}),
		deadlineUpdated: make(chan struct{}),
	}

	channel.SetBufferedAmountLowThreshold(bufferedAmountLowThreshold)
	channel.OnBufferedAmountLow(func() {
		result.m.Lock()
		writeAvailable := result.writeAvailable
		result.writeAvailable = make(chan struct{})
		result.m.Unlock()
		close(writeAvailable)
	})

	result.wg.Add(1)
	go result.readLoop()

	return result
}

func (d *dataChannel) Read(b []byte) (int, error) {
	timeout := make(chan struct{})
	var deadlineTimer *time.Timer
	first := true
	for {
		d.m.Lock()
		read := copy(b, d.readBuf)
		d.readBuf = d.readBuf[read:]
		remaining := len(d.readBuf)
		d.m.Unlock()
		if state := d.getState(); remaining == 0 && (state == stateReadClosed || state == stateClosed) {
			return read, io.EOF
		}
		if read > 0 {
			return read, nil
		}

		// read until data message and only queue read request once
		if first {
			first = false
			d.requestRead <- struct{}{}
		}

		d.m.Lock()
		deadlineUpdated := d.deadlineUpdated
		deadline := d.readDeadline
		d.m.Unlock()
		if !deadline.IsZero() {
			if deadline.Before(time.Now()) {
				return 0, os.ErrDeadlineExceeded
			}
			if deadlineTimer == nil {
				deadlineTimer = time.AfterFunc(time.Until(deadline), func() { close(timeout) })
				defer deadlineTimer.Stop()
			}
			deadlineTimer.Reset(time.Until(deadline))
		}

		select {
		case <-d.receivedMessage:
		case <-timeout:
			return 0, os.ErrDeadlineExceeded
		case <-deadlineUpdated:
		case <-d.ctx.Done():
		}
	}
}

func (d *dataChannel) Write(b []byte) (int, error) {
	state := d.getState()
	if state == stateWriteClosed || state == stateClosed {
		return 0, io.ErrClosedPipe
	}

	// Check if there is any message on the wire. This is used for control
	// messages only
	if state == stateReadClosed {
		// drain the channel
		select {
		case <-d.receivedMessage:
		default:
		}
		// async push a read request to the channel
		select {
		case d.requestRead <- struct{}{}:
		default:
		}
	}

	var err error
	var (
		chunkSize = maxMessageSize - protoOverhead - varintOverhead
		n         = 0
	)

	for len(b) > 0 {
		end := min(chunkSize, len(b))

		written, err := d.partialWrite(b[:end])
		if err != nil {
			return n + written, err
		}
		b = b[end:]
		n += written
	}
	return n, err
}

func (d *dataChannel) partialWrite(b []byte) (int, error) {

	// if the next message will add more data than we are willing to buffer,
	// block until we have sent enough bytes to reduce the amount of data buffered.
	timeout := make(chan struct{})
	var deadlineTimer *time.Timer
	for {
		if s := d.getState(); s == stateWriteClosed || s == stateClosed {
			return 0, io.ErrClosedPipe
		}
		d.m.Lock()
		deadline := d.writeDeadline
		deadlineUpdated := d.deadlineUpdated
		writeAvailable := d.writeAvailable
		d.m.Unlock()
		if !deadline.IsZero() {
			// check if deadline exceeded
			if deadline.Before(time.Now()) {
				return 0, os.ErrDeadlineExceeded
			}

			if deadlineTimer == nil {
				deadlineTimer = time.AfterFunc(time.Until(deadline), func() { close(timeout) })
				defer deadlineTimer.Stop()
			}
			deadlineTimer.Reset(time.Until(deadline))
		}

		msg := &pb.Message{Message: b}
		bufferedAmount := int(d.channel.BufferedAmount()) + len(b) + protoOverhead + varintOverhead
		if bufferedAmount > maxBufferedAmount {
			select {
			case <-timeout:
				return 0, os.ErrDeadlineExceeded
			case <-writeAvailable:
				return d.writeMessage(msg)
			case <-d.ctx.Done():
				return 0, io.ErrClosedPipe
			case <-deadlineUpdated:

			}
		} else {
			return d.writeMessage(msg)
		}
	}
}

func (d *dataChannel) writeMessage(msg *pb.Message) (int, error) {
	err := d.writer.WriteMsg(msg)
	// this only returns the number of bytes sent from the buffer
	// requested by the user.
	return len(msg.GetMessage()), err

}

func (d *dataChannel) Close() error {
	select {
	case <-d.ctx.Done():
		return nil
	default:
	}

	d.m.Lock()
	d.state = stateClosed
	d.m.Unlock()

	d.cancelFunc()
	_ = d.CloseWrite()
	_ = d.channel.Close()
	// this does not loop and call Close again
	d.wg.Wait()
	return nil
}

func (d *dataChannel) CloseRead() error {
	var err error
	d.closeReadOnce.Do(func() {
		d.m.Lock()
		previousState := d.state
		currentState := d.state.processOutgoingFlag(pb.Message_STOP_SENDING)
		d.state = currentState
		d.m.Unlock()
		if previousState != currentState && currentState == stateClosed {
			defer d.Close()
		}
		msg := &pb.Message{
			Flag: pb.Message_STOP_SENDING.Enum(),
		}
		err = d.writer.WriteMsg(msg)
	})
	return err

}

func (d *dataChannel) remoteClosed() {
	d.m.Lock()
	defer d.m.Unlock()
	d.state = stateClosed
	d.cancelFunc()

}

func (d *dataChannel) CloseWrite() error {
	var err error
	d.closeWriteOnce.Do(func() {
		d.m.Lock()
		previousState := d.state
		currentState := d.state.processOutgoingFlag(pb.Message_FIN)
		d.state = currentState
		d.m.Unlock()
		if previousState != currentState && currentState == stateClosed {
			defer d.Close()
		}
		msg := &pb.Message{
			Flag: pb.Message_FIN.Enum(),
		}
		err = d.writer.WriteMsg(msg)
	})
	return err
}

func (d *dataChannel) LocalAddr() net.Addr {
	return d.laddr
}

func (d *dataChannel) RemoteAddr() net.Addr {
	return d.raddr
}

func (d *dataChannel) Reset() error {
	var err error
	d.resetOnce.Do(func() {
		msg := &pb.Message{Flag: pb.Message_RESET.Enum()}
		_, err = d.writeMessage(msg)
		err = d.Close()
	})
	return err
}

func (d *dataChannel) SetDeadline(t time.Time) error {
	d.m.Lock()
	defer d.m.Unlock()
	d.readDeadline = t
	d.writeDeadline = t
	return nil
}

func (d *dataChannel) SetReadDeadline(t time.Time) error {
	d.m.Lock()
	d.readDeadline = t
	deadlineUpdated := d.deadlineUpdated
	d.deadlineUpdated = make(chan struct{})
	d.m.Unlock()
	close(deadlineUpdated)
	return nil
}

func (d *dataChannel) SetWriteDeadline(t time.Time) error {
	d.m.Lock()
	d.writeDeadline = t
	deadlineUpdated := d.deadlineUpdated
	d.deadlineUpdated = make(chan struct{})
	d.m.Unlock()
	close(deadlineUpdated)
	return nil
}

func (d *dataChannel) getState() channelState {
	d.m.Lock()
	defer d.m.Unlock()
	return d.state
}

func (d *dataChannel) readLoop() {
	defer d.wg.Done()
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-d.requestRead:
		}

		var msg pb.Message
		err := d.reader.ReadMsg(&msg)
		if err != nil {
			log.Errorf("[channel %d] could not read message: %v", *d.channel.ID(), err)
			return
		}

		d.m.Lock()
		if d.state != stateClosed && d.state != stateReadClosed && msg.Message != nil {
			d.readBuf = append(d.readBuf, msg.Message...)
		}
		previous := d.state
		current := d.state
		if msg.Flag != nil {
			current = d.state.handleIncomingFlag(msg.GetFlag())
		}
		d.state = current
		d.m.Unlock()
		d.receivedMessage <- struct{}{}

		if previous != current && current == stateClosed {
			d.Close()
		}

	}
}
