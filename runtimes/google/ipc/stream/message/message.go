// Package message provides data structures and serialization/deserialization
// methods for messages exchanged by the implementation of the
// veyron2/ipc/stream interfaces in veyron/runtimes/google/ipc/stream.
package message

// This file contains methods to read and write messages sent over the VIF.
// Every message has the following format:
//
// +-----------------------------------------+
// | Type (1 byte) | PayloadSize (3 bytes)   |
// +-----------------------------------------+
// | Payload (PayloadSize bytes)             |
// +-----------------------------------------+
//
// Currently, there are 2 valid types:
// 0 (controlType)
// 1 (dataType)
//
// When Type == controlType, the message is:
// +---------------------------------------------+
// |      0        | PayloadSize (3 bytes)       |
// +---------------------------------------------+
// | Cmd  (1 byte)                               |
// +---------------------------------------------+
// | Data (PayloadSize - MACSize - 1 bytes)      |
// +---------------------------------------------+
// | MAC (MACSize bytes)                         |
// +---------------------------------------------+
// Where Data is the serialized Control interface object.
//
// When Type == dataType, the message is:
// +---------------------------------------------+
// |      1        | PayloadSize (3 bytes)       |
// +---------------------------------------------+
// | id.VCI (4 bytes)                            |
// +---------------------------------------------+
// | id.Flow (4 bytes)                           |
// +---------------------------------------------+
// | Flags (1 byte)                              |
// +---------------------------------------------+
// | MAC (MACSize bytes)                         |
// +---------------------------------------------+
// | Data (PayloadSize - 9 - MACSize bytes)      |
// +---------------------------------------------+
// Where Data is the application data.  The Data is encrypted separately; it is
// not included in the MAC.
//
// A crypto.ControlCipher is used to encrypt the control data.  The MACSize
// comes from the ControlCipher.  When used, the first word of the header,
// containing the Type and PayloadSize, is encrypted with the cipher's Encrypt
// method.  The rest of the control data is encrypted with the cipher's Seal
// method.  This means that none of the data is observable by an adversary, but
// the type and length are subject to corruption (the rest of the data is not).
// This doesn't matter -- if the Type or PayloadSize is corrupted by an
// adversary, the payload will be misread, and will fail to validate.
//
// We could potentially pass the Type and PayloadSize in the clear, but then the
// framing would be observable, a (probably minor) information leak.  There is
// no reason to do so, we encrypt everything.

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"v.io/veyron/veyron/runtimes/google/ipc/stream/crypto"
	"v.io/veyron/veyron/runtimes/google/ipc/stream/id"
	"v.io/veyron/veyron/runtimes/google/lib/iobuf"
	"v.io/veyron/veyron2/vlog"
)

const (
	// Size (in bytes) of headers appended to application data payload in
	// Data messages.
	HeaderSizeBytes = commonHeaderSizeBytes + dataHeaderSizeBytes

	commonHeaderSizeBytes = 4 // 1 byte type + 3 bytes payload length
	dataHeaderSizeBytes   = 9 // 4 byte id.VC + 4 byte id.Flow + 1 byte flags

	// Make sure the first byte can't be ASCII to ensure that a VC
	// header can never be confused with a web socket request.
	// TODO(cnicolaou): remove the original controlType and dataType values
	// when new binaries are pushed.
	controlType   = 0
	controlTypeWS = 0x80
	dataType      = 1
	dataTypeWS    = 0x81
)

var (
	emptyMessageErr     = errors.New("message is empty")
	corruptedMessageErr = errors.New("corrupted message")
)

// T is the interface implemented by all messages communicated over a VIF.
type T interface {
}

// ReadFrom reads a message from the provided iobuf.Reader.
//
// Sample usage:
//	msg, err := message.ReadFrom(r)
//	switch m := msg.(type) {
//		case *Data:
//			notifyFlowOfReceivedData(m.VCI, m.Flow, m.Payload)
//			if m.Closed() {
//			   closeFlow(m.VCI, m.Flow)
//			}
//		case Control:
//			handleControlMessage(m)
//	}
func ReadFrom(r *iobuf.Reader, c crypto.ControlCipher) (T, error) {
	header, err := r.Read(commonHeaderSizeBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to read VC header: %v", err)
	}
	c.Decrypt(header.Contents)
	msgType := header.Contents[0]
	msgPayloadSize := read3ByteUint(header.Contents[1:4])
	header.Release()
	payload, err := r.Read(msgPayloadSize)
	if err != nil {
		return nil, fmt.Errorf("failed to read payload of %d bytes for type %d: %v", msgPayloadSize, msgType, err)
	}
	macSize := c.MACSize()
	switch msgType {
	case controlType, controlTypeWS:
		if !c.Open(payload.Contents) {
			payload.Release()
			return nil, corruptedMessageErr
		}
		m, err := readControl(bytes.NewBuffer(payload.Contents[:msgPayloadSize-macSize]))
		payload.Release()
		return m, err
	case dataType, dataTypeWS:
		if !c.Open(payload.Contents[0 : dataHeaderSizeBytes+macSize]) {
			payload.Release()
			return nil, corruptedMessageErr
		}
		m := &Data{
			VCI:     id.VC(read4ByteUint(payload.Contents[0:4])),
			Flow:    id.Flow(read4ByteUint(payload.Contents[4:8])),
			flags:   payload.Contents[8],
			Payload: payload,
		}
		m.Payload.TruncateFront(uint(dataHeaderSizeBytes + macSize))
		return m, nil
	default:
		payload.Release()
		return nil, fmt.Errorf("unrecognized message type: %d", msgType)
	}
}

// WriteTo serializes message and makes a single call to w.Write.
// It is the inverse of ReadFrom.
//
// By writing the message in a single call to w.Write, confusion is avoided in
// case multiple goroutines are calling Write on w simultaneously.
//
// If message is a Data message, the Payload contents will be Released
// irrespective of the return value of this method.
func WriteTo(w io.Writer, message T, c crypto.ControlCipher) error {
	macSize := c.MACSize()
	switch m := message.(type) {
	case *Data:
		payloadSize := m.PayloadSize() + dataHeaderSizeBytes + macSize
		msg := mkHeaderSpace(m.Payload, uint(HeaderSizeBytes+macSize))
		header := msg.Contents[0 : HeaderSizeBytes+macSize]
		header[0] = dataType
		if err := write3ByteUint(header[1:4], payloadSize); err != nil {
			return err

		}
		write4ByteUint(header[4:8], uint32(m.VCI))
		write4ByteUint(header[8:12], uint32(m.Flow))
		header[12] = m.flags
		EncryptMessage(msg.Contents, c)
		_, err := w.Write(msg.Contents)
		msg.Release()
		return err
	case Control:
		var buf bytes.Buffer
		// Prevent a few memory allocations by presizing the buffer to
		// something that is large enough for typical control messages.
		buf.Grow(256)
		// Reserve space for the header
		if err := extendBuffer(&buf, commonHeaderSizeBytes); err != nil {
			return err
		}
		if err := writeControl(&buf, m); err != nil {
			return err
		}
		if err := extendBuffer(&buf, macSize); err != nil {
			return err
		}
		msg := buf.Bytes()
		msg[0] = controlType
		if err := write3ByteUint(msg[1:4], buf.Len()-commonHeaderSizeBytes); err != nil {
			return err
		}
		EncryptMessage(msg, c)
		_, err := w.Write(msg)
		return err
	default:
		return fmt.Errorf("invalid message type %T", m)
	}
	return nil
}

// EncryptMessage encrypts the message's control data in place.
func EncryptMessage(msg []byte, c crypto.ControlCipher) error {
	if len(msg) == 0 {
		return emptyMessageErr
	}
	n := len(msg)
	switch msgType := msg[0]; msgType {
	case controlType:
		// skip
	case dataType:
		n = HeaderSizeBytes + c.MACSize()
	default:
		return fmt.Errorf("unrecognized message type: %d", msgType)
	}
	c.Encrypt(msg[0:commonHeaderSizeBytes])
	c.Seal(msg[commonHeaderSizeBytes:n])
	return nil
}

func mkHeaderSpace(slice *iobuf.Slice, space uint) *iobuf.Slice {
	if slice == nil {
		return iobuf.NewSlice(make([]byte, space))
	}
	if slice.ExpandFront(space) {
		return slice
	}
	vlog.VI(10).Infof("Failed to expand slice by %d bytes. Copying", space)
	contents := make([]byte, slice.Size()+int(space))
	copy(contents[space:], slice.Contents)
	slice.Release()
	return iobuf.NewSlice(contents)
}

var emptyBytes [256]byte

func extendBuffer(buf *bytes.Buffer, size int) error {
	_, err := buf.Write(emptyBytes[:size])
	return err
}
