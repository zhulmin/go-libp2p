package libp2pwebrtc

import (
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
	maxMessageSize uint64 = 16384
	// Max message size limit in the SDP is limited to 16384 bytes.
	// We keep a maximum of 2 messages in the buffer
	maxBufferedAmount uint64 = 3 * maxMessageSize
	// bufferedAmountLowThreshold and maxBufferedAmount are bound
	// to a stream but congestion control is done on the whole
	// SCTP association. This means that a single stream can monopolize
	// the complete congestion control window (cwnd) if it does not
	// read stream data and it's remote continues to send. We can
	// add messages to the send buffer once there is space for 1 full
	// sized message.
	bufferedAmountLowThreshold uint64 = 16384

	protoOverhead  int = 5
	varintOverhead int = 2
)

// Package pion detached data channel into a net.Conn
// and then a network.MuxedStream
type dataChannel struct {
	channel       *webrtc.DataChannel
	rwc           datachannel.ReadWriteCloser
	laddr         net.Addr
	raddr         net.Addr
	readDeadline  time.Time
	writeDeadline time.Time

	closeWriteOnce sync.Once
	closeReadOnce  sync.Once
	resetOnce      sync.Once

	state channelState

	ctx            context.Context
	cancel         context.CancelFunc
	m              sync.Mutex
	readBuf        []byte
	writeAvailable chan struct{}
	reader         protoio.Reader
	writer         protoio.Writer
}

func newDataChannel(
	channel *webrtc.DataChannel,
	rwc datachannel.ReadWriteCloser,
	pc *webrtc.PeerConnection,
	laddr, raddr net.Addr) *dataChannel {
	ctx, cancel := context.WithCancel(context.Background())

	result := &dataChannel{
		channel:        channel,
		rwc:            rwc,
		laddr:          laddr,
		raddr:          raddr,
		readDeadline:   time.Time{},
		writeDeadline:  time.Time{},
		ctx:            ctx,
		cancel:         cancel,
		writeAvailable: make(chan struct{}),
		reader:         protoio.NewDelimitedReader(rwc, 16384),
		writer:         protoio.NewDelimitedWriter(rwc),
		readBuf:        []byte{},
	}

	channel.SetBufferedAmountLowThreshold(bufferedAmountLowThreshold)
	channel.OnBufferedAmountLow(func() {
		result.writeAvailable <- struct{}{}
	})

	return result
}

func (d *dataChannel) processControlMessage(msg pb.Message) {
	if msg.Flag == nil {
		return
	}
	d.m.Lock()
	previous := d.state
	current := d.state.handleIncomingFlag(msg.GetFlag())
	d.state = current
	defer d.m.Unlock()
	// no state transition
	if current == stateClosed && previous != current {
		_ = d.Close()
	}
}

func (d *dataChannel) Read(b []byte) (int, error) {
	for {
		if state := d.getState(); state == stateClosed || state == stateReadClosed {
			return 0, io.EOF
		}

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

		// read until data message
		var msg pb.Message
		err := d.reader.ReadMsg(&msg)
		if err != nil {
			return 0, err
		}

		if state := d.getState(); state != stateClosed && state != stateReadClosed && msg.Message != nil {
			d.m.Lock()
			d.readBuf = append(d.readBuf, msg.Message...)
			d.m.Unlock()
		}

		d.processControlMessage(msg)
	}
}

func (d *dataChannel) Write(b []byte) (int, error) {
	if s := d.getState(); s == stateWriteClosed || s == stateClosed {
		return 0, io.ErrClosedPipe
	}
	var err error
	var (
		chunkSize = int(maxMessageSize) - protoOverhead - varintOverhead
		n         = 0
	)

	for len(b) > 0 {

		d.m.Lock()
		dl := d.writeDeadline
		if !dl.IsZero() && time.Now().After(dl) {
			d.m.Unlock()
			return 0, os.ErrDeadlineExceeded
		}
		d.m.Unlock()

		// check timer
		end := chunkSize
		if len(b) < end {
			end = len(b)
		}

		written, err := d.partialWrite(b[:end])
		if err != nil {
			break
		}
		b = b[end:]
		n += written
	}
	return n, err
}

func (d *dataChannel) partialWrite(b []byte) (int, error) {
	if s := d.getState(); s == stateWriteClosed || s == stateClosed {
		return 0, io.ErrClosedPipe
	}

	timedOut := make(chan struct{})
	d.m.Lock() 
	dl := d.writeDeadline
	d.m.Unlock()
	if !dl.IsZero() {
		timer := time.AfterFunc(time.Until(dl), func() { close(timedOut) })
		defer timer.Stop()
	}

	msg := &pb.Message{Message: b}
	// if the next message will add more data than we are willing to buffer,
	// block until we have sent enough bytes to reduce the amount of data buffered.
	if d.channel.BufferedAmount()+uint64(len(b))+uint64(varintOverhead) > maxBufferedAmount {
		select {
		case <-timedOut:
			return 0, os.ErrDeadlineExceeded
		case <-d.writeAvailable:
		}
	}
	return d.writeMessage(msg)
}

func (d *dataChannel) writeMessage(msg *pb.Message) (int, error) {
	err := d.writer.WriteMsg(msg)
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

	d.cancel()
	d.CloseWrite()
	_ = d.channel.Close()
	return nil
}

func (d *dataChannel) CloseRead() error {
	var err error
	d.closeReadOnce.Do(func() {
		d.m.Lock()
		if d.state != stateClosed {
			d.state = stateReadClosed
		}
		d.m.Unlock()
		msg := &pb.Message{
			Flag: pb.Message_STOP_SENDING.Enum(),
		}
		_, err = d.writeMessage(msg)
	})
	return err

}

func (d *dataChannel) remoteClosed() {
	d.m.Lock()
	defer d.m.Unlock()
	d.state = stateClosed
	d.cancel()

}

func (d *dataChannel) CloseWrite() error {
	var err error
	d.closeWriteOnce.Do(func() {
		d.m.Lock()
		if d.state != stateClosed {
			d.state = stateWriteClosed
		}
		d.m.Unlock()
		msg := &pb.Message{
			Flag: pb.Message_FIN.Enum(),
		}
		_, err = d.writeMessage(msg)
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
		d.Close()
	})
	return err
}

func (d *dataChannel) SetDeadline(t time.Time) error {
	d.SetReadDeadline(t)
	d.SetWriteDeadline(t)
	return nil
}

func (d *dataChannel) SetReadDeadline(t time.Time) error {
	d.m.Lock()
	defer d.m.Unlock()
	d.readDeadline = t
	return nil
}

func (d *dataChannel) SetWriteDeadline(t time.Time) error {
	d.m.Lock()
	defer d.m.Unlock()
	d.writeDeadline = t
	return nil
}

func (d *dataChannel) getState() channelState {
	d.m.Lock()
	defer d.m.Unlock()
	return d.state
}
