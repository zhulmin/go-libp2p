package libp2pwebrtc

import (
	"bufio"
	"context"
	"errors"
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
	// Pion SCTP association has an internal receive buffer of 1MB (roughly, 1MB per connection).
	// We can change this value in the SettingEngine before creating the peerconnection.
	// https://github.com/pion/webrtc/blob/v3.1.49/sctptransport.go#L341
	maxBufferedAmount int = 2 * maxMessageSize
	// bufferedAmountLowThreshold and maxBufferedAmount are bound
	// to a stream but congestion control is done on the whole
	// SCTP association. This means that a single stream can monopolize
	// the complete congestion control window (cwnd) if it does not
	// read stream data and it's remote continues to send. We can
	// add messages to the send buffer once there is space for 1 full
	// sized message.
	bufferedAmountLowThreshold uint64 = uint64(maxBufferedAmount) / 2

	protoOverhead  int = 5
	varintOverhead int = 2
)

// Package pion detached data channel into a net.Conn
// and then a network.MuxedStream
type dataChannel struct {
	conn  *connection
	id    uint16
	rwc   datachannel.ReadWriteCloser
	laddr net.Addr
	raddr net.Addr

	closeWriteOnce sync.Once
	closeReadOnce  sync.Once
	resetOnce      sync.Once
	readLoopOnce   sync.Once

	state channelState

	reader protoio.Reader
	writer protoio.Writer

	m               sync.Mutex
	readBuf         []byte
	writeDeadline   time.Time
	writeAvailable  chan struct{}
	deadlineUpdated chan struct{}

	// hack for closing the Read side using a deadline
	// in case `Read` does not return.
	closeErr error

	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

func newDataChannel(
	connection *connection,
	channel *webrtc.DataChannel,
	rwc datachannel.ReadWriteCloser,
	laddr, raddr net.Addr) *dataChannel {
	ctx, cancel := context.WithCancel(context.Background())

	reader := bufio.NewReaderSize(rwc, maxMessageSize)

	result := &dataChannel{
		conn:            connection,
		id:              *channel.ID(),
		rwc:             rwc,
		laddr:           laddr,
		raddr:           raddr,
		ctx:             ctx,
		cancel:          cancel,
		writeAvailable:  make(chan struct{}),
		reader:          protoio.NewDelimitedReader(reader, maxMessageSize),
		writer:          protoio.NewDelimitedWriter(rwc),
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

	return result
}

func (d *dataChannel) Read(b []byte) (int, error) {
	for {
		// check if buffer has data
		if len(b) == 0 {
			return 0, nil
		}

		d.m.Lock()
		read := copy(b, d.readBuf)
		d.readBuf = d.readBuf[read:]
		remaining := len(d.readBuf)
		d.m.Unlock()

		if state := d.getState(); remaining == 0 && (state == stateReadClosed || state == stateClosed) {
			d.m.Lock()
			defer d.m.Unlock()
			closeErr := d.closeErr
			if closeErr != nil {
				return read, closeErr
			}
			return read, io.EOF
		}

		if read > 0 {
			return read, nil
		}

		// read from datachannel
		var msg pb.Message
		err := d.reader.ReadMsg(&msg)
		if err != nil {
			// This case occurs when the remote node goes away
			// without writing a FIN message
			if errors.Is(err, io.EOF) {
				d.remoteClosed()
				if d.conn != nil {
					d.conn.removeStream(d.id)
				}
				return 0, io.ErrClosedPipe
			}
			if errors.Is(err, os.ErrDeadlineExceeded) {
				d.m.Lock()
				defer d.m.Unlock()
				if d.closeErr != nil {
					return 0, d.closeErr
				}
			}
			return 0, err
		}

		d.m.Lock()
		if d.state != stateClosed && d.state != stateReadClosed && msg.Message != nil {
			d.readBuf = append(d.readBuf, msg.GetMessage()...)
		}
		d.m.Unlock()

		if msg.Flag != nil {
			d.processIncomingFlag(msg.GetFlag())
		}
	}
}

func (d *dataChannel) processIncomingFlag(flag pb.Message_Flag) {
	d.m.Lock()
	current := d.state
	next := d.state.handleIncomingFlag(flag)
	d.state = next
	d.m.Unlock()

	if current != next && next == stateClosed {
		if flag == pb.Message_RESET {
			defer d.Reset()
			return
		}
		defer d.Close()
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
		d.readLoopOnce.Do(func() {
			d.wg.Add(1)
			go func() {
				defer d.wg.Done()
				// zero the read deadline, so read call only returns
				// when the underlying datachannel closes or there is
				// a message on the channel
				d.rwc.(*datachannel.DataChannel).SetReadDeadline(time.Time{})
				var msg pb.Message
				for {
					err := d.reader.ReadMsg(&msg)
					if err != nil {
						if errors.Is(err, io.EOF) {
							d.remoteClosed()
						}
						return
					}
					if msg.Flag != nil {
						d.processIncomingFlag(msg.GetFlag())
					}
				}
			}()
		})
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
		bufferedAmount := int(d.rwc.(*datachannel.DataChannel).BufferedAmount())
		addedBuffer := bufferedAmount + len(b) + protoOverhead + varintOverhead
		if addedBuffer > maxBufferedAmount {
			select {
			case <-timeout:
				return 0, os.ErrDeadlineExceeded
			case <-writeAvailable:
				err := d.writer.WriteMsg(msg)
				if err != nil {
					return 0, err
				}
				return len(b), nil
			case <-d.ctx.Done():
				return 0, io.ErrClosedPipe
			case <-deadlineUpdated:

			}
		} else {
			err := d.writer.WriteMsg(msg)
			if err != nil {
				return 0, err
			}
			return len(b), nil
		}
	}
}

func (d *dataChannel) Close() error {
	select {
	case <-d.ctx.Done():
		return nil
	default:
	}

	d.m.Lock()
	d.state = stateClosed
	d.closeErr = io.EOF
	d.m.Unlock()
	if d.conn != nil {
		d.conn.removeStream(d.id)
	}

	// Since this is a valid close, we close the write side
	// to signal EOF to the remote
	d.CloseWrite()
	d.cancel()
	// This is a hack. A recent commit in pion/datachannel
	// caused a regression where read blocks indefinitely
	// even when then channel is closed.
	// PR being worked on here: https://github.com/pion/datachannel/pull/161
	// This method allows any read calls to fail with
	// deadline exceeded and unblocks them.
	d.rwc.(*datachannel.DataChannel).SetReadDeadline(time.Now().Add(-100 * time.Second))
	err := d.rwc.Close()
	d.wg.Wait()
	return err
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
	// remove stream
	defer func() {
		if d.conn != nil {
			d.conn.removeStream(d.id)
		}
	}()
	d.m.Lock()
	defer d.m.Unlock()
	d.state = stateClosed
	d.closeErr = io.ErrClosedPipe
	close(d.writeAvailable)
	d.cancel()
}

func (d *dataChannel) CloseWrite() error {
	var err error
	d.closeWriteOnce.Do(func() {
		d.m.Lock()
		previousState := d.state
		currentState := d.state.processOutgoingFlag(pb.Message_FIN)
		d.state = currentState
		writeAvailable := d.writeAvailable
		d.writeAvailable = make(chan struct{})
		d.m.Unlock()
		close(writeAvailable)
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

func (d *dataChannel) reset() error {
	var err error
	d.resetOnce.Do(func() {
		msg := &pb.Message{Flag: pb.Message_RESET.Enum()}
		_ = d.writer.WriteMsg(msg)
		d.m.Lock()
		d.state = stateClosed
		d.closeErr = io.ErrClosedPipe
		writeAvailable := d.writeAvailable
		d.writeAvailable = make(chan struct{})
		d.m.Unlock()
		close(writeAvailable)
		// hack to force close the read
		d.rwc.(*datachannel.DataChannel).SetReadDeadline(time.Now().Add(-100 * time.Millisecond))
	})
	return err
}

func (d *dataChannel) Reset() error {
	if d.conn != nil {
		d.conn.removeStream(d.id)
	}
	return d.reset()
}

func (d *dataChannel) SetDeadline(t time.Time) error {
	d.m.Lock()
	defer d.m.Unlock()
	d.writeDeadline = t
	return nil
}

func (d *dataChannel) SetReadDeadline(t time.Time) error {
	select {
	case <-d.ctx.Done():
		return nil
	default:
	}
	return d.rwc.(*datachannel.DataChannel).SetReadDeadline(t)
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
