package message

import (
	"bytes"
	"fmt"
	"io"

	"veyron/runtimes/google/ipc/stream/id"
	inaming "veyron/runtimes/google/naming"
	"veyron2/naming"
)

// Control is the interface implemented by all control messages.
type Control interface {
	readFrom(r io.Reader) error
	writeTo(w io.Writer) error
}

// OpenVC is a Control implementation requesting the creation of a new virtual
// circuit.
type OpenVC struct {
	VCI         id.VC
	DstEndpoint naming.Endpoint
	SrcEndpoint naming.Endpoint
	Counters    Counters
}

// CloseVC is a Control implementation notifying the closure of an established
// virtual circuit.
// The Error string will be empty in case the close was the result of an
// explicit close by the application (and not an error).
type CloseVC struct {
	VCI   id.VC
	Error string
}

// AddReceiveBuffers is a Control implementation used by the sender of the
// message to inform the other end of a virtual circuit that it is ready to
// receive more bytes of data (specified per flow).
type AddReceiveBuffers struct {
	Counters Counters
}

// OpenFlow is a Control implementation notifying the senders intent to create
// a new Flow. It also include the number of bytes the sender of this message
// is willing to read.
type OpenFlow struct {
	VCI             id.VC
	Flow            id.Flow
	InitialCounters uint32
}

type command uint8

const (
	openVCCommand            command = 0
	closeVCCommand           command = 1
	addReceiveBuffersCommand command = 2
	openFlowCommand          command = 3
)

func writeControl(w io.Writer, m Control) error {
	var command command
	switch m.(type) {
	case *OpenVC:
		command = openVCCommand
	case *CloseVC:
		command = closeVCCommand
	case *AddReceiveBuffers:
		command = addReceiveBuffersCommand
	case *OpenFlow:
		command = openFlowCommand
	default:
		return fmt.Errorf("unrecognized VC control message: %T", m)
	}
	var header [1]byte
	header[0] = byte(command)
	if n, err := w.Write(header[:]); n != len(header) || err != nil {
		return fmt.Errorf("failed to write header. Got (%d, %v) want (%d, nil)", n, err, len(header))
	}
	if err := m.writeTo(w); err != nil {
		return err
	}
	return nil
}

func readControl(r *bytes.Buffer) (Control, error) {
	var header byte
	var err error
	if header, err = r.ReadByte(); err != nil {
		return nil, fmt.Errorf("message too small, cannot read control message command (0, %v)", err)
	}
	command := command(header)
	var m Control
	switch command {
	case openVCCommand:
		m = new(OpenVC)
	case closeVCCommand:
		m = new(CloseVC)
	case addReceiveBuffersCommand:
		m = new(AddReceiveBuffers)
	case openFlowCommand:
		m = new(OpenFlow)
	default:
		return nil, fmt.Errorf("unrecognized VC control message command(%d)", command)
	}
	if err := m.readFrom(r); err != nil {
		return nil, fmt.Errorf("failed to deserialize control message %d(%T): %v", command, m, err)
	}
	return m, nil
}

func (m *OpenVC) writeTo(w io.Writer) (err error) {
	if err = writeInt(w, m.VCI); err != nil {
		return
	}
	if err = writeString(w, m.DstEndpoint.String()); err != nil {
		return
	}
	if err = writeString(w, m.SrcEndpoint.String()); err != nil {
		return
	}
	if err = writeCounters(w, m.Counters); err != nil {
		return
	}
	return nil
}

func (m *OpenVC) readFrom(r io.Reader) (err error) {
	if err = readInt(r, &m.VCI); err != nil {
		return
	}
	var ep string
	if err = readString(r, &ep); err != nil {
		return
	}
	if m.DstEndpoint, err = inaming.NewEndpoint(ep); err != nil {
		return
	}
	if err = readString(r, &ep); err != nil {
		return
	}
	if m.SrcEndpoint, err = inaming.NewEndpoint(ep); err != nil {
		return
	}
	if m.Counters, err = readCounters(r); err != nil {
		return
	}
	return nil
}

func (m *CloseVC) writeTo(w io.Writer) (err error) {
	if err = writeInt(w, m.VCI); err != nil {
		return
	}
	if err = writeString(w, m.Error); err != nil {
		return
	}
	return
}

func (m *CloseVC) readFrom(r io.Reader) (err error) {
	if err = readInt(r, &m.VCI); err != nil {
		return
	}
	if err = readString(r, &m.Error); err != nil {
		return
	}
	return
}

func (m *AddReceiveBuffers) writeTo(w io.Writer) error {
	return writeCounters(w, m.Counters)
}

func (m *AddReceiveBuffers) readFrom(r io.Reader) (err error) {
	m.Counters, err = readCounters(r)
	return
}

func (m *OpenFlow) writeTo(w io.Writer) (err error) {
	if err = writeInt(w, m.VCI); err != nil {
		return
	}
	if err = writeInt(w, m.Flow); err != nil {
		return
	}
	if err = writeInt(w, m.InitialCounters); err != nil {
		return
	}
	return
}

func (m *OpenFlow) readFrom(r io.Reader) (err error) {
	if err = readInt(r, &m.VCI); err != nil {
		return
	}
	if err = readInt(r, &m.Flow); err != nil {
		return
	}
	if err = readInt(r, &m.InitialCounters); err != nil {
		return
	}
	return
}
