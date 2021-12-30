package powerstrip

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeroFruit/powerstrip/mux"
)

type muxBrokerPending struct {
	ch     chan net.Conn
	doneCh chan struct{}
}

type MuxBroker struct {
	nextId  uint32
	session *mux.Session
	streams map[uint32]*muxBrokerPending

	sync.Mutex
}

func newMuxBroker(s *mux.Session) *MuxBroker {
	return &MuxBroker{
		session: s,
		streams: make(map[uint32]*muxBrokerPending),
	}
}

func (m *MuxBroker) Accept(id uint32) (net.Conn, error) {
	var c net.Conn
	p := m.getStream(id)
	select {
	case c = <-p.ch:
		close(p.doneCh)
	case <-time.After(5 * time.Second):
		m.Lock()
		defer m.Unlock()
		delete(m.streams, id)

		return nil, fmt.Errorf("timeout waiting for accept")
	}

	// Ack our connection
	if err := binary.Write(c, binary.LittleEndian, id); err != nil {
		c.Close()
		return nil, err
	}

	return c, nil
}

func (m *MuxBroker) getStream(id uint32) *muxBrokerPending {
	m.Lock()
	defer m.Unlock()

	p, ok := m.streams[id]
	if ok {
		return p
	}

	m.streams[id] = &muxBrokerPending{
		ch:     make(chan net.Conn, 1),
		doneCh: make(chan struct{}),
	}
	return m.streams[id]
}

func (m *MuxBroker) Close() error {
	return m.session.Close()
}

func (m *MuxBroker) Dial(id uint32) (net.Conn, error) {
	stream, err := m.session.OpenStream()
	if err != nil {
		return nil, err
	}
	// Write the stream ID onto the wire.
	if err := binary.Write(stream, binary.LittleEndian, id); err != nil {
		stream.Close()
		return nil, err
	}
	// Read the ack that we connected. Then we're off!
	var ack uint32
	if err := binary.Read(stream, binary.LittleEndian, &ack); err != nil {
		stream.Close()
		return nil, err
	}
	if ack != id {
		stream.Close()
		return nil, fmt.Errorf("bad ack: %d (expected %d)", ack, id)
	}
	return stream, nil
}

func (m *MuxBroker) NextId() uint32 {
	return atomic.AddUint32(&m.nextId, 1)
}

func (m *MuxBroker) Run() {
	for {
		stream, err := m.session.AcceptStream()
		if err != nil {
			// Once we receive an error, just exit
			break
		}

		// Read the stream ID from the stream
		var id uint32
		if err := binary.Read(stream, binary.LittleEndian, &id); err != nil {
			stream.Close()
			continue
		}

		// Initialize the waiter
		p := m.getStream(id)
		select {
		case p.ch <- stream:
		default:
		}

		// Wait for a timeout
		go m.timeoutWait(id, p)
	}
}

func (m *MuxBroker) timeoutWait(id uint32, p *muxBrokerPending) {
	// Wait for the stream to either be picked up and connected, or
	// for a timeout.
	timeout := false
	select {
	case <-p.doneCh:
	case <-time.After(5 * time.Second):
		timeout = true
	}

	m.Lock()
	defer m.Unlock()

	// Delete the stream so no one else can grab it
	delete(m.streams, id)

	// If we timed out, then check if we have a channel in the buffer,
	// and if so, close it.
	if timeout {
		select {
		case s := <-p.ch:
			s.Close()
		}
	}
}
