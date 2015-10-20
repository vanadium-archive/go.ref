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

	inaming "v.io/x/ref/runtime/internal/naming"
	"v.io/x/ref/runtime/internal/rpc/stream/crypto"
	"v.io/x/ref/runtime/internal/rpc/stream/id"
	"v.io/x/ref/runtime/internal/rpc/version"
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

// SetupVC is a Control implementation containing information to setup a new
// virtual circuit.
type SetupVC struct {
	VCI            id.VC
	LocalEndpoint  naming.Endpoint // Endpoint of the sender (as seen by the sender), can be nil.
	RemoteEndpoint naming.Endpoint // Endpoint of the receiver (as seen by the sender), can be nil.
	Counters       Counters
	Setup          Setup // Negotiate versioning and encryption.
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

// SetupStream is a byte stream used to negotiate VIF setup. During VIF setup,
// each party sends a Setup message to the other party containing their version
// and options. If the version requires further negotiation (such as for
// authentication), the SetupStream is used for the negotiation.
//
// The protocol used on the stream is version-specific, it is not specified here.
// See vif/auth.go for an example.
type SetupStream struct {
	Data []byte
}

// HealthCheckRequest is used to periodically check to see if the remote end
// is still available.
type HealthCheckRequest struct {
	VCI id.VC
}

// HealthCheckResponse is sent in response to a health check request.
type HealthCheckResponse struct {
	VCI id.VC
}

// Command enum.
type command uint8

const (
	closeVCCommand           command = 1
	addReceiveBuffersCommand command = 2
	openFlowCommand          command = 3
	setupCommand             command = 4
	setupStreamCommand       command = 5
	setupVCCommand           command = 6
	healthCheckReqCommand    command = 7
	healthCheckRespCommand   command = 8
)

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

// Setup option codes.
type setupOptionCode uint16

const (
	naclBoxOptionCode              setupOptionCode = 0
	peerEndpointOptionCode         setupOptionCode = 1
	useVIFAuthenticationOptionCode setupOptionCode = 2
)

// NaclBox is a SetupOption that specifies the public key for the NaclBox
// encryption protocol.
type NaclBox struct {
	PublicKey crypto.BoxKey
}

// PeerEndpoint is a SetupOption that exchanges the endpoints between peers.
type PeerEndpoint struct {
	LocalEndpoint naming.Endpoint // Endpoint of the sender (as seen by the sender).
}

// UseVIFAuthentication is a SetupOption that notifies the server to use
// the VIF authentication for the new virtual circuit.
type UseVIFAuthentication struct {
	// Signature for binding a principal to a channel to make sure that the peer
	// who requests to use VIF authentication is the same peer of the VIF.
	Signature []byte
}

func writeControl(w io.Writer, m Control) error {
	var command command
	switch m.(type) {
	case *CloseVC:
		command = closeVCCommand
	case *AddReceiveBuffers:
		command = addReceiveBuffersCommand
	case *OpenFlow:
		command = openFlowCommand
	case *Setup:
		command = setupCommand
	case *SetupStream:
		command = setupStreamCommand
	case *SetupVC:
		command = setupVCCommand
	case *HealthCheckRequest:
		command = healthCheckReqCommand
	case *HealthCheckResponse:
		command = healthCheckRespCommand
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
	case closeVCCommand:
		m = new(CloseVC)
	case addReceiveBuffersCommand:
		m = new(AddReceiveBuffers)
	case openFlowCommand:
		m = new(OpenFlow)
	case setupCommand:
		m = new(Setup)
	case setupStreamCommand:
		m = new(SetupStream)
	case setupVCCommand:
		m = new(SetupVC)
	case healthCheckReqCommand:
		m = new(HealthCheckRequest)
	case healthCheckRespCommand:
		m = new(HealthCheckResponse)
	default:
		return nil, verror.New(errUnrecognizedVCControlMessageCommand, nil, command)
	}
	if err := m.readFrom(r); err != nil {
		return nil, verror.New(errFailedToDeserializedVCControlMessage, nil, command, fmt.Sprintf("%T", m), err)
	}
	return m, nil
}

func (m *CloseVC) writeTo(w io.Writer) (err error) {
	if err = writeInt(w, m.VCI); err != nil {
		return
	}
	err = writeString(w, m.Error)
	return
}

func (m *CloseVC) readFrom(r *bytes.Buffer) (err error) {
	if err = readInt(r, &m.VCI); err != nil {
		return
	}
	m.Error, err = readString(r)
	return
}

func (m *HealthCheckRequest) writeTo(w io.Writer) (err error) {
	return writeInt(w, m.VCI)
}

func (m *HealthCheckRequest) readFrom(r *bytes.Buffer) (err error) {
	return readInt(r, &m.VCI)
}

func (m *HealthCheckResponse) writeTo(w io.Writer) (err error) {
	return writeInt(w, m.VCI)
}

func (m *HealthCheckResponse) readFrom(r *bytes.Buffer) (err error) {
	return readInt(r, &m.VCI)
}

func (m *SetupVC) writeTo(w io.Writer) (err error) {
	if err = writeInt(w, m.VCI); err != nil {
		return
	}
	var ep string
	if m.LocalEndpoint != nil {
		ep = m.LocalEndpoint.String()
	}
	if err = writeString(w, ep); err != nil {
		return
	}
	if m.RemoteEndpoint != nil {
		ep = m.RemoteEndpoint.String()
	}
	if err = writeString(w, ep); err != nil {
		return
	}
	if err = writeCounters(w, m.Counters); err != nil {
		return
	}
	err = m.Setup.writeTo(w)
	return
}

func (m *SetupVC) readFrom(r *bytes.Buffer) (err error) {
	if err = readInt(r, &m.VCI); err != nil {
		return
	}
	var ep string
	if ep, err = readString(r); err != nil {
		return
	}
	if len(ep) > 0 {
		if m.LocalEndpoint, err = inaming.NewEndpoint(ep); err != nil {
			return
		}
	}
	if ep, err = readString(r); err != nil {
		return
	}
	if len(ep) > 0 {
		if m.RemoteEndpoint, err = inaming.NewEndpoint(ep); err != nil {
			return
		}
	}
	if m.Counters, err = readCounters(r); err != nil {
		return
	}
	err = m.Setup.readFrom(r)
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
	err = writeInt(w, m.InitialCounters)
	return
}

func (m *OpenFlow) readFrom(r *bytes.Buffer) (err error) {
	if err = readInt(r, &m.VCI); err != nil {
		return
	}
	if err = readInt(r, &m.Flow); err != nil {
		return
	}
	err = readInt(r, &m.InitialCounters)
	return
}

func (m *Setup) writeTo(w io.Writer) (err error) {
	if err = writeInt(w, m.Versions.Min); err != nil {
		return
	}
	if err = writeInt(w, m.Versions.Max); err != nil {
		return
	}
	err = writeSetupOptions(w, m.Options)
	return
}

func (m *Setup) readFrom(r *bytes.Buffer) (err error) {
	if err = readInt(r, &m.Versions.Min); err != nil {
		return
	}
	if err = readInt(r, &m.Versions.Max); err != nil {
		return
	}
	m.Options, err = readSetupOptions(r)
	return
}

func (m *SetupStream) writeTo(w io.Writer) error {
	_, err := w.Write(m.Data)
	return err
}

func (m *SetupStream) readFrom(r *bytes.Buffer) error {
	m.Data = r.Bytes()
	return nil
}

// NaclBox returns the first NaclBox option, or nil if there is none.
func (m *Setup) NaclBox() *NaclBox {
	for _, opt := range m.Options {
		if o, ok := opt.(*NaclBox); ok {
			return o
		}
	}
	return nil
}

func (*NaclBox) code() setupOptionCode {
	return naclBoxOptionCode
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

// PeerEndpoint returns the naming.Endpoint in the first PeerEndpoint
// option, or nil if there is none.
func (m *Setup) PeerEndpoint() naming.Endpoint {
	for _, opt := range m.Options {
		if o, ok := opt.(*PeerEndpoint); ok {
			return o.LocalEndpoint
		}
	}
	return nil
}

func (*PeerEndpoint) code() setupOptionCode {
	return peerEndpointOptionCode
}

func (m *PeerEndpoint) size() uint16 {
	var ep string
	if m.LocalEndpoint != nil {
		ep = m.LocalEndpoint.String()
	}
	return uint16(sizeOfSizeT + len(ep))
}

func (m *PeerEndpoint) write(w io.Writer) error {
	var ep string
	if m.LocalEndpoint != nil {
		ep = m.LocalEndpoint.String()
	}
	return writeString(w, ep)
}

func (m *PeerEndpoint) read(r io.Reader) (err error) {
	var ep string
	if ep, err = readString(r); err != nil {
		return
	}
	if len(ep) > 0 {
		m.LocalEndpoint, err = inaming.NewEndpoint(ep)
	}
	return
}

// UseVIFAuthentication returns the signature of the first UseVIFAuthentication
// option, or nil if there is none.
func (m *Setup) UseVIFAuthentication() []byte {
	for _, opt := range m.Options {
		if o, ok := opt.(*UseVIFAuthentication); ok {
			return o.Signature
		}
	}
	return nil
}

func (*UseVIFAuthentication) code() setupOptionCode {
	return useVIFAuthenticationOptionCode
}

func (m *UseVIFAuthentication) size() uint16 {
	return uint16(sizeOfSizeT + len(m.Signature))
}

func (m *UseVIFAuthentication) write(w io.Writer) error {
	return writeBytes(w, m.Signature)
}

func (m *UseVIFAuthentication) read(r io.Reader) (err error) {
	m.Signature, err = readBytes(r)
	return
}
