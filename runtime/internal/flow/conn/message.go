// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc/version"
)

// TODO(mattr): Link to protocol doc.

type message interface {
	write(ctx *context.T, p *messagePipe) error
	read(ctx *context.T, data []byte) error
}

// message types.
const (
	invalidType = iota
	controlType
	dataType
	unencryptedDataType
)

// control commands.
const (
	invalidCmd = iota
	setupCmd
	tearDownCmd
	openFlowCmd
	releaseCmd
)

// setup options.
const (
	invalidOption = iota
	peerNaClPublicKeyOption
	peerRemoteEndpointOption
	peerLocalEndpointOption
)

// data flags.
const (
	closeFlag = 1 << iota
	metadataFlag
)

// random consts.
const (
	maxVarUint64Size = 9
)

// setup is the first message over the wire.  It negotiates protocol version
// and encryption options for connection.
type setup struct {
	versions           version.RPCVersionRange
	PeerNaClPublicKey  *[32]byte
	PeerRemoteEndpoint naming.Endpoint
	PeerLocalEndpoint  naming.Endpoint
}

func (m *setup) write(ctx *context.T, p *messagePipe) error {
	p.controlBuf = writeVarUint64(uint64(m.versions.Min), p.controlBuf[:0])
	p.controlBuf = writeVarUint64(uint64(m.versions.Max), p.controlBuf)
	return p.write([][]byte{{controlType}}, [][]byte{{setupCmd}, p.controlBuf})
}
func (m *setup) read(ctx *context.T, orig []byte) error {
	var (
		data  = orig
		valid bool
		v     uint64
	)
	if v, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidControlMsg(ctx, setupCmd, int64(len(orig)), 0)
	}
	m.versions.Min = version.RPCVersion(v)
	if v, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidControlMsg(ctx, setupCmd, int64(len(orig)), 1)
	}
	m.versions.Max = version.RPCVersion(v)
	return nil
}

// tearDown is sent over the wire before a connection is closed.
type tearDown struct {
	Message string
}

func (m *tearDown) write(ctx *context.T, p *messagePipe) error {
	return p.write([][]byte{{controlType}}, [][]byte{{tearDownCmd}, []byte(m.Message)})
}
func (m *tearDown) read(ctx *context.T, data []byte) error {
	if len(data) > 0 {
		m.Message = string(data)
	}
	return nil
}

// openFlow is sent at the beginning of every new flow.
type openFlow struct {
	id              flowID
	initialCounters uint64
}

func (m *openFlow) write(ctx *context.T, p *messagePipe) error {
	p.controlBuf = writeVarUint64(uint64(m.id), p.controlBuf[:0])
	p.controlBuf = writeVarUint64(m.initialCounters, p.controlBuf)
	return p.write([][]byte{{controlType}}, [][]byte{{openFlowCmd}, p.controlBuf})
}
func (m *openFlow) read(ctx *context.T, orig []byte) error {
	var (
		data  = orig
		valid bool
		v     uint64
	)
	if v, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidControlMsg(ctx, openFlowCmd, int64(len(orig)), 0)
	}
	m.id = flowID(v)
	if m.initialCounters, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidControlMsg(ctx, openFlowCmd, int64(len(orig)), 1)
	}
	return nil
}

// release is sent as flows are read from locally.  The counters
// inform remote writers that there is local buffer space available.
type release struct {
	counters map[flowID]uint64
}

func (m *release) write(ctx *context.T, p *messagePipe) error {
	p.controlBuf = p.controlBuf[:0]
	for fid, val := range m.counters {
		p.controlBuf = writeVarUint64(uint64(fid), p.controlBuf)
		p.controlBuf = writeVarUint64(val, p.controlBuf)
	}
	return p.write([][]byte{{controlType}}, [][]byte{{releaseCmd}, p.controlBuf})
}
func (m *release) read(ctx *context.T, orig []byte) error {
	var (
		data     = orig
		valid    bool
		fid, val uint64
		n        int64
	)
	if len(data) == 0 {
		return nil
	}
	m.counters = map[flowID]uint64{}
	for len(data) > 0 {
		if fid, data, valid = readVarUint64(ctx, data); !valid {
			return NewErrInvalidControlMsg(ctx, releaseCmd, int64(len(orig)), n)
		}
		if val, data, valid = readVarUint64(ctx, data); !valid {
			return NewErrInvalidControlMsg(ctx, releaseCmd, int64(len(orig)), n+1)
		}
		m.counters[flowID(fid)] = val
		n += 2
	}
	return nil
}

// data carries encrypted data for a specific flow.
type data struct {
	id      flowID
	flags   uint64
	payload [][]byte
}

func (m *data) write(ctx *context.T, p *messagePipe) error {
	p.dataBuf = writeVarUint64(uint64(m.id), p.dataBuf[:0])
	p.dataBuf = writeVarUint64(m.flags, p.dataBuf)
	encrypted := append([][]byte{p.dataBuf}, m.payload...)
	return p.write([][]byte{{dataType}}, encrypted)
}
func (m *data) read(ctx *context.T, orig []byte) error {
	var (
		data  = orig
		valid bool
		v     uint64
	)
	if v, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidMsg(ctx, dataType, int64(len(orig)), 0)
	}
	m.id = flowID(v)
	if m.flags, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidMsg(ctx, dataType, int64(len(orig)), 1)
	}
	if len(data) > 0 {
		m.payload = [][]byte{data}
	}
	return nil
}

// unencryptedData carries unencrypted data for a specific flow.
type unencryptedData struct {
	id      flowID
	flags   uint64
	payload [][]byte
}

func (m *unencryptedData) write(ctx *context.T, p *messagePipe) error {
	p.dataBuf = writeVarUint64(uint64(m.id), p.dataBuf[:0])
	p.dataBuf = writeVarUint64(m.flags, p.dataBuf)
	// re-use the controlBuf for the data size.
	size := uint64(0)
	for _, b := range m.payload {
		size += uint64(len(b))
	}
	p.controlBuf = writeVarUint64(size, p.controlBuf[:0])
	unencrypted := append([][]byte{[]byte{unencryptedDataType}, p.controlBuf}, m.payload...)
	return p.write(unencrypted, [][]byte{p.dataBuf})
}
func (m *unencryptedData) read(ctx *context.T, orig []byte) error {
	var (
		data  = orig
		valid bool
		v     uint64
	)
	if v, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidMsg(ctx, unencryptedDataType, int64(len(orig)), 0)
	}
	plen := int(v)
	if plen > len(data) {
		return NewErrInvalidMsg(ctx, unencryptedDataType, int64(len(orig)), 1)
	}
	if plen > 0 {
		m.payload, data = [][]byte{data[:plen]}, data[plen:]
	}
	if v, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidMsg(ctx, unencryptedDataType, int64(len(orig)), 2)
	}
	m.id = flowID(v)
	if m.flags, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidMsg(ctx, unencryptedDataType, int64(len(orig)), 3)
	}
	return nil
}

type messagePipe struct {
	rw         MsgReadWriteCloser
	controlBuf []byte
	dataBuf    []byte
	outBuf     [][]byte
}

func newMessagePipe(rw MsgReadWriteCloser) *messagePipe {
	return &messagePipe{
		rw:         rw,
		controlBuf: make([]byte, 256),
		dataBuf:    make([]byte, 2*maxVarUint64Size),
		outBuf:     make([][]byte, 5),
	}
}

func (p *messagePipe) close() error {
	return p.rw.Close()
}

func (p *messagePipe) write(unencrypted [][]byte, encrypted [][]byte) error {
	p.outBuf = append(p.outBuf[:0], unencrypted...)
	p.outBuf = append(p.outBuf, encrypted...)
	_, err := p.rw.WriteMsg(p.outBuf...)
	return err
}

func (p *messagePipe) writeMsg(ctx *context.T, m message) error {
	return m.write(ctx, p)
}

func (p *messagePipe) readMsg(ctx *context.T) (message, error) {
	msg, err := p.rw.ReadMsg()
	if len(msg) == 0 {
		return nil, NewErrInvalidMsg(ctx, invalidType, 0, 0)
	}
	if err != nil {
		return nil, err
	}
	var m message
	switch msg[0] {
	case controlType:
		if len(msg) == 1 {
			return nil, NewErrInvalidControlMsg(ctx, invalidCmd, 0, 1)
		}
		msg = msg[1:]
		switch msg[0] {
		case setupCmd:
			m = &setup{}
		case tearDownCmd:
			m = &tearDown{}
		case openFlowCmd:
			m = &openFlow{}
		case releaseCmd:
			m = &release{}
		default:
			return nil, NewErrUnknownControlMsg(ctx, msg[0])
		}
	case dataType:
		m = &data{}
	case unencryptedDataType:
		m = &unencryptedData{}
	default:
		return nil, NewErrUnknownMsg(ctx, msg[0])
	}
	return m, m.read(ctx, msg[1:])
}

func readVarUint64(ctx *context.T, data []byte) (uint64, []byte, bool) {
	if len(data) == 0 {
		return 0, data, false
	}
	l := data[0]
	if l <= 0x7f {
		return uint64(l), data[1:], true
	}
	l = 0xff - l + 1
	if l > 8 || len(data)-1 < int(l) {
		return 0, data, false
	}
	var out uint64
	for i := 1; i < int(l+1); i++ {
		out = out<<8 | uint64(data[i])
	}
	return out, data[l+1:], true
}

func writeVarUint64(u uint64, buf []byte) []byte {
	if u <= 0x7f {
		return append(buf, byte(u))
	}
	shift, l := 56, byte(7)
	for ; shift >= 0 && (u>>uint(shift))&0xff == 0; shift, l = shift-8, l-1 {
	}
	buf = append(buf, 0xff-l)
	for ; shift >= 0; shift -= 8 {
		buf = append(buf, byte(u>>uint(shift))&0xff)
	}
	return buf
}
