// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package message

import (
	"bytes"
	"fmt"
	"io"

	"v.io/v23/naming"
	"v.io/v23/verror"

	inaming "v.io/x/ref/profiles/internal/naming"
	"v.io/x/ref/profiles/internal/rpc/stream/crypto"
	"v.io/x/ref/profiles/internal/rpc/stream/id"
	"v.io/x/ref/profiles/internal/rpc/version"
)

var (
	// These errors are intended to be used as arguments to higher
	// level errors and hence {1}{2} is omitted from their format
	// strings to avoid repeating these n-times in the final error
	// message visible to the user.
	errUnrecognizedVCControlMessageCommand = reg(".errUnrecognizedVCControlMessageCommand",
		"unrecognized VC control message command({3})")
	errUnrecognizedVCControlMessageType = reg(".errUnrecognizedVCControlMessageType",
		"unrecognized VC control message type({3})")
	errFailedToDeserializedVCControlMessage = reg(".errFailedToDeserializedVCControlMessage", "failed to deserialize control message {3}({4}): {5}")
	errFailedToWriteHeader                  = reg(".errFailedToWriteHeader", "failed to write header. Wrote {3} bytes instead of {4}{:5}")
)

// Control is the interface implemented by all control messages.
type Control interface {
	readFrom(r *bytes.Buffer) error
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
// virtual circuit, or failure to establish a virtual circuit.
//
// The Error string will be empty in case the close was the result of an
// explicit close by the application (and not an error).
type CloseVC struct {
	VCI   id.VC
	Error string
}

// SetupVC is a Control implementation containing information to setup a new
// virtual circuit. This message is expected to replace OpenVC and allow for
// the two ends of a VC to establish a protocol version.
type SetupVC struct {
	VCI            id.VC
	LocalEndpoint  naming.Endpoint // Endpoint of the sender (as seen by the sender), can be nil.
	RemoteEndpoint naming.Endpoint // Endpoint of the receiver (as seen by the sender), can be nil.
	Counters       Counters
	Setup          Setup // Negotiate versioning and encryption.
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

// Setup is a control message used to negotiate VIF/VC options.
type Setup struct {
	Versions version.Range
	Options  []SetupOption
}

// SetupOption is the base interface for optional Setup options.
type SetupOption interface {
	// code is the identifier for the option.
	code() setupOptionCode

	// size returns the number of bytes needed to represent the option.
	size() uint16

	// write the option to the writer.
	write(w io.Writer) error

	// read the option from the reader.
	read(r io.Reader) error
}

// NaclBox is a SetupOption that specifies the public key for the NaclBox
// encryption protocol.
type NaclBox struct {
	PublicKey crypto.BoxKey
}

// SetupStream is a byte stream used to negotiate VIF setup.  During VIF setup,
// each party sends a Setup message to the other party containing their version
// and options.  If the version requires further negotiation (such as for authentication),
// the SetupStream is used for the negotiation.
//
// The protocol used on the stream is version-specific, it is not specified here.  See
// vif/auth.go for an example.
type SetupStream struct {
	Data []byte
}

// Setup option codes.
type setupOptionCode uint16

const (
	naclBoxPublicKey setupOptionCode = 0
)

// Command enum.
type command uint8

const (
	openVCCommand            command = 0
	closeVCCommand           command = 1
	addReceiveBuffersCommand command = 2
	openFlowCommand          command = 3
	hopSetupCommand          command = 4
	hopSetupStreamCommand    command = 5
	setupVCCommand           command = 6
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
	case *Setup:
		command = hopSetupCommand
	case *SetupStream:
		command = hopSetupStreamCommand
	case *SetupVC:
		command = setupVCCommand
	default:
		return verror.New(errUnrecognizedVCControlMessageType, nil, fmt.Sprintf("%T", m))
	}
	var header [1]byte
	header[0] = byte(command)
	if n, err := w.Write(header[:]); n != len(header) || err != nil {
		return verror.New(errFailedToWriteHeader, nil, n, len(header), err)
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
		return nil, err
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
	case hopSetupCommand:
		m = new(Setup)
	case hopSetupStreamCommand:
		m = new(SetupStream)
	case setupVCCommand:
		m = new(SetupVC)
	default:
		return nil, verror.New(errUnrecognizedVCControlMessageCommand, nil, command)
	}
	if err := m.readFrom(r); err != nil {
		return nil, verror.New(errFailedToDeserializedVCControlMessage, nil, command, fmt.Sprintf("%T", m), err)
	}
	return m, nil
}

func (m *OpenVC) writeTo(w io.Writer) (err error) {
	if err = writeInt(w, m.VCI); err != nil {
		return
	}
	// Note that when we send OpenVC we always use v4 endpoints because
	// the server needs to get version information from them.
	if err = writeString(w, m.DstEndpoint.VersionedString(4)); err != nil {
		return
	}
	if err = writeString(w, m.SrcEndpoint.VersionedString(4)); err != nil {
		return
	}
	if err = writeCounters(w, m.Counters); err != nil {
		return
	}
	return nil
}

func (m *OpenVC) readFrom(r *bytes.Buffer) (err error) {
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

func (m *CloseVC) readFrom(r *bytes.Buffer) (err error) {
	if err = readInt(r, &m.VCI); err != nil {
		return
	}
	if err = readString(r, &m.Error); err != nil {
		return
	}
	return
}

func (m *SetupVC) writeTo(w io.Writer) (err error) {
	if err = writeInt(w, m.VCI); err != nil {
		return
	}
	var localep string
	if m.LocalEndpoint != nil {
		localep = m.LocalEndpoint.String()
	}
	if err = writeString(w, localep); err != nil {
		return
	}
	var remoteep string
	if m.RemoteEndpoint != nil {
		remoteep = m.RemoteEndpoint.String()
	}
	if err = writeString(w, remoteep); err != nil {
		return
	}
	if err = writeCounters(w, m.Counters); err != nil {
		return
	}
	if err = m.Setup.writeTo(w); err != nil {
		return
	}
	return
}

func (m *SetupVC) readFrom(r *bytes.Buffer) (err error) {
	if err = readInt(r, &m.VCI); err != nil {
		return
	}
	var ep string
	if err = readString(r, &ep); err != nil {
		return
	}
	if ep != "" {
		if m.LocalEndpoint, err = inaming.NewEndpoint(ep); err != nil {
			return
		}
	}
	if err = readString(r, &ep); err != nil {
		return
	}
	if ep != "" {
		if m.RemoteEndpoint, err = inaming.NewEndpoint(ep); err != nil {
			return
		}
	}
	if m.Counters, err = readCounters(r); err != nil {
		return
	}
	if err = m.Setup.readFrom(r); err != nil {
		return
	}
	return
}

func (m *AddReceiveBuffers) writeTo(w io.Writer) error {
	return writeCounters(w, m.Counters)
}

func (m *AddReceiveBuffers) readFrom(r *bytes.Buffer) (err error) {
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

func (m *OpenFlow) readFrom(r *bytes.Buffer) (err error) {
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

func (m *Setup) writeTo(w io.Writer) (err error) {
	if err = writeInt(w, m.Versions.Min); err != nil {
		return
	}
	if err = writeInt(w, m.Versions.Max); err != nil {
		return
	}
	if err = writeSetupOptions(w, m.Options); err != nil {
		return
	}
	return
}

func (m *Setup) readFrom(r *bytes.Buffer) (err error) {
	if err = readInt(r, &m.Versions.Min); err != nil {
		return
	}
	if err = readInt(r, &m.Versions.Max); err != nil {
		return
	}
	if m.Options, err = readSetupOptions(r); err != nil {
		return
	}
	return
}

// NaclBox returns the first NaclBox option, or nil if there is none.
func (m *Setup) NaclBox() *NaclBox {
	for _, opt := range m.Options {
		if b, ok := opt.(*NaclBox); ok {
			return b
		}
	}
	return nil
}

func (*NaclBox) code() setupOptionCode {
	return naclBoxPublicKey
}

func (m *NaclBox) size() uint16 {
	return uint16(len(m.PublicKey))
}

func (m *NaclBox) write(w io.Writer) error {
	_, err := w.Write(m.PublicKey[:])
	return err
}

func (m *NaclBox) read(r io.Reader) error {
	_, err := io.ReadFull(r, m.PublicKey[:])
	return err
}

func (m *SetupStream) writeTo(w io.Writer) error {
	_, err := w.Write(m.Data)
	return err
}

func (m *SetupStream) readFrom(r *bytes.Buffer) error {
	m.Data = r.Bytes()
	return nil
}
