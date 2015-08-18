// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc/version"
	"v.io/v23/security"
	"v.io/x/ref/runtime/internal/rpc/stream/crypto"
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
	authCmd
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
)

// random consts.
const (
	maxVarUint64Size = 9
)

// setup is the first message over the wire.  It negotiates protocol version
// and encryption options for connection.
type setup struct {
	versions           version.RPCVersionRange
	peerNaClPublicKey  *[32]byte
	peerRemoteEndpoint naming.Endpoint
	peerLocalEndpoint  naming.Endpoint
}

func writeSetupOption(option uint64, payload, buf []byte) []byte {
	buf = writeVarUint64(option, buf)
	buf = writeVarUint64(uint64(len(payload)), buf)
	return append(buf, payload...)
}
func readSetupOption(ctx *context.T, orig []byte) (
	option uint64, payload, data []byte, err error) {
	var valid bool
	if option, data, valid = readVarUint64(ctx, orig); !valid {
		err = NewErrInvalidSetupOption(ctx, invalidOption, 0)
		return
	}
	var size uint64
	if size, data, valid = readVarUint64(ctx, data); !valid || uint64(len(data)) < size {
		err = NewErrInvalidSetupOption(ctx, option, 1)
		return
	}
	payload, data = data[:size], data[size:]
	return
}

func (m *setup) write(ctx *context.T, p *messagePipe) error {
	p.controlBuf = writeVarUint64(uint64(m.versions.Min), p.controlBuf[:0])
	p.controlBuf = writeVarUint64(uint64(m.versions.Max), p.controlBuf)
	if m.peerNaClPublicKey != nil {
		p.controlBuf = writeSetupOption(peerNaClPublicKeyOption,
			m.peerNaClPublicKey[:], p.controlBuf)
	}
	if m.peerRemoteEndpoint != nil {
		p.controlBuf = writeSetupOption(peerRemoteEndpointOption,
			[]byte(m.peerRemoteEndpoint.String()), p.controlBuf)
	}
	if m.peerLocalEndpoint != nil {
		p.controlBuf = writeSetupOption(peerLocalEndpointOption,
			[]byte(m.peerLocalEndpoint.String()), p.controlBuf)
	}
	return p.write(ctx, [][]byte{{controlType, setupCmd}, p.controlBuf})
}
func (m *setup) read(ctx *context.T, orig []byte) error {
	var (
		data  = orig
		valid bool
		v     uint64
	)
	if v, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidControlMsg(ctx, setupCmd, uint64(len(orig)), 0, nil)
	}
	m.versions.Min = version.RPCVersion(v)
	if v, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidControlMsg(ctx, setupCmd, uint64(len(orig)), 1, nil)
	}
	m.versions.Max = version.RPCVersion(v)
	for field := uint64(2); len(data) > 0; field++ {
		var (
			payload []byte
			option  uint64
			err     error
		)
		if option, payload, data, err = readSetupOption(ctx, data); err != nil {
			return NewErrInvalidControlMsg(ctx, setupCmd, uint64(len(orig)), field, err)
		}
		switch option {
		case peerNaClPublicKeyOption:
			m.peerNaClPublicKey = new([32]byte)
			copy(m.peerNaClPublicKey[:], payload)
		case peerRemoteEndpointOption:
			m.peerRemoteEndpoint, err = v23.NewEndpoint(string(payload))
		case peerLocalEndpointOption:
			m.peerLocalEndpoint, err = v23.NewEndpoint(string(payload))
		default:
			err = NewErrUnknownSetupOption(ctx, option)
		}
		if err != nil {
			return NewErrInvalidControlMsg(ctx, setupCmd, uint64(len(orig)), field, err)
		}
	}
	return nil
}

// tearDown is sent over the wire before a connection is closed.
type tearDown struct {
	Message string
}

func (m *tearDown) write(ctx *context.T, p *messagePipe) error {
	return p.write(ctx, [][]byte{{controlType, tearDownCmd}, []byte(m.Message)})
}
func (m *tearDown) read(ctx *context.T, data []byte) error {
	if len(data) > 0 {
		m.Message = string(data)
	}
	return nil
}

// auth is used to complete the auth handshake.
type auth struct {
	bkey, dkey     uint64
	channelBinding security.Signature
	publicKey      security.PublicKey
}

func (m *auth) write(ctx *context.T, p *messagePipe) error {
	p.controlBuf = writeVarUint64(m.bkey, p.controlBuf[:0])
	p.controlBuf = writeVarUint64(m.dkey, p.controlBuf)
	s := m.channelBinding
	p.controlBuf = writeVarUint64(uint64(len(s.Purpose)), p.controlBuf)
	p.controlBuf = append(p.controlBuf, s.Purpose...)
	p.controlBuf = writeVarUint64(uint64(len(s.Hash)), p.controlBuf)
	p.controlBuf = append(p.controlBuf, []byte(s.Hash)...)
	p.controlBuf = writeVarUint64(uint64(len(s.R)), p.controlBuf)
	p.controlBuf = append(p.controlBuf, s.R...)
	p.controlBuf = writeVarUint64(uint64(len(s.S)), p.controlBuf)
	p.controlBuf = append(p.controlBuf, s.S...)
	if m.publicKey != nil {
		pk, err := m.publicKey.MarshalBinary()
		if err != nil {
			return err
		}
		p.controlBuf = append(p.controlBuf, pk...)
	}
	return p.write(ctx, [][]byte{{controlType, authCmd}, p.controlBuf})
}
func (m *auth) read(ctx *context.T, orig []byte) error {
	var data []byte
	var valid bool
	var l uint64
	if m.bkey, data, valid = readVarUint64(ctx, orig); !valid {
		return NewErrInvalidControlMsg(ctx, openFlowCmd, uint64(len(orig)), 0, nil)
	}
	if m.dkey, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidControlMsg(ctx, openFlowCmd, uint64(len(orig)), 1, nil)
	}
	if l, data, valid = readVarUint64(ctx, data); !valid || uint64(len(data)) < l {
		return NewErrInvalidControlMsg(ctx, openFlowCmd, uint64(len(orig)), 2, nil)
	}
	if l > 0 {
		m.channelBinding.Purpose, data = data[:l], data[l:]
	}
	if l, data, valid = readVarUint64(ctx, data); !valid || uint64(len(data)) < l {
		return NewErrInvalidControlMsg(ctx, openFlowCmd, uint64(len(orig)), 3, nil)
	}
	if l > 0 {
		m.channelBinding.Hash, data = security.Hash(data[:l]), data[l:]
	}
	if l, data, valid = readVarUint64(ctx, data); !valid || uint64(len(data)) < l {
		return NewErrInvalidControlMsg(ctx, openFlowCmd, uint64(len(orig)), 4, nil)
	}
	if l > 0 {
		m.channelBinding.R, data = data[:l], data[l:]
	}
	if l, data, valid = readVarUint64(ctx, data); !valid || uint64(len(data)) < l {
		return NewErrInvalidControlMsg(ctx, openFlowCmd, uint64(len(orig)), 5, nil)
	}
	if l > 0 {
		m.channelBinding.S, data = data[:l], data[l:]
	}
	if len(data) > 0 {
		var err error
		m.publicKey, err = security.UnmarshalPublicKey(data)
		return err
	}
	return nil
}

// openFlow is sent at the beginning of every new flow.
type openFlow struct {
	id              flowID
	initialCounters uint64
	bkey, dkey      uint64
}

func (m *openFlow) write(ctx *context.T, p *messagePipe) error {
	p.controlBuf = writeVarUint64(uint64(m.id), p.controlBuf[:0])
	p.controlBuf = writeVarUint64(m.initialCounters, p.controlBuf)
	p.controlBuf = writeVarUint64(m.bkey, p.controlBuf)
	p.controlBuf = writeVarUint64(m.dkey, p.controlBuf)
	return p.write(ctx, [][]byte{{controlType, openFlowCmd}, p.controlBuf})
}
func (m *openFlow) read(ctx *context.T, orig []byte) error {
	var (
		data  = orig
		valid bool
		v     uint64
	)
	if v, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidControlMsg(ctx, openFlowCmd, uint64(len(orig)), 0, nil)
	}
	m.id = flowID(v)
	if m.initialCounters, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidControlMsg(ctx, openFlowCmd, uint64(len(orig)), 1, nil)
	}
	if m.bkey, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidControlMsg(ctx, openFlowCmd, uint64(len(orig)), 2, nil)
	}
	if m.dkey, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidControlMsg(ctx, openFlowCmd, uint64(len(orig)), 3, nil)
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
	return p.write(ctx, [][]byte{{controlType, releaseCmd}, p.controlBuf})
}
func (m *release) read(ctx *context.T, orig []byte) error {
	var (
		data     = orig
		valid    bool
		fid, val uint64
		n        uint64
	)
	if len(data) == 0 {
		return nil
	}
	m.counters = map[flowID]uint64{}
	for len(data) > 0 {
		if fid, data, valid = readVarUint64(ctx, data); !valid {
			return NewErrInvalidControlMsg(ctx, releaseCmd, uint64(len(orig)), n, nil)
		}
		if val, data, valid = readVarUint64(ctx, data); !valid {
			return NewErrInvalidControlMsg(ctx, releaseCmd, uint64(len(orig)), n+1, nil)
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
	p.controlBuf = writeVarUint64(uint64(m.id), p.controlBuf[:0])
	p.controlBuf = writeVarUint64(m.flags, p.controlBuf)
	return p.write(ctx, append([][]byte{{dataType}, p.controlBuf}, m.payload...))
}
func (m *data) read(ctx *context.T, orig []byte) error {
	var (
		data  = orig
		valid bool
		v     uint64
	)
	if v, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidMsg(ctx, dataType, uint64(len(orig)), 0)
	}
	m.id = flowID(v)
	if m.flags, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidMsg(ctx, dataType, uint64(len(orig)), 1)
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
	p.controlBuf = writeVarUint64(uint64(m.id), p.controlBuf[:0])
	p.controlBuf = writeVarUint64(m.flags, p.controlBuf)
	if err := p.write(ctx, [][]byte{{unencryptedDataType}, p.controlBuf}); err != nil {
		return err
	}
	_, err := p.rw.WriteMsg(m.payload...)
	return err
}
func (m *unencryptedData) read(ctx *context.T, orig []byte) error {
	var (
		data  = orig
		valid bool
		v     uint64
	)
	if v, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidMsg(ctx, unencryptedDataType, uint64(len(orig)), 2)
	}
	m.id = flowID(v)
	if m.flags, data, valid = readVarUint64(ctx, data); !valid {
		return NewErrInvalidMsg(ctx, unencryptedDataType, uint64(len(orig)), 3)
	}
	return nil
}

// TODO(mattr): Consider cleaning up the ControlCipher library to
// eliminate extraneous functionality and reduce copying.
type messagePipe struct {
	rw         MsgReadWriteCloser
	cipher     crypto.ControlCipher
	controlBuf []byte
	encBuf     []byte
}

func newMessagePipe(rw MsgReadWriteCloser) *messagePipe {
	return &messagePipe{
		rw:         rw,
		controlBuf: make([]byte, 256),
		encBuf:     make([]byte, mtu),
		cipher:     &crypto.NullControlCipher{},
	}
}

func (p *messagePipe) setupEncryption(ctx *context.T, pk, sk, opk *[32]byte) []byte {
	p.cipher = crypto.NewControlCipherRPC11(
		(*crypto.BoxKey)(pk),
		(*crypto.BoxKey)(sk),
		(*crypto.BoxKey)(opk))
	return p.cipher.ChannelBinding()
}

func (p *messagePipe) close() error {
	return p.rw.Close()
}

func (p *messagePipe) write(ctx *context.T, encrypted [][]byte) error {
	// TODO(mattr): Because of the API of the underlying crypto library,
	// an enormous amount of copying happens here.
	// TODO(mattr): We allocate many buffers here to hold potentially
	// many copies of the data.  The maximum memory usage per Conn is probably
	// quite high.  We should try to reduce it.
	needed := p.cipher.MACSize()
	for _, b := range encrypted {
		needed += len(b)
	}
	if cap(p.encBuf) < needed {
		p.encBuf = make([]byte, needed)
	}
	p.encBuf = p.encBuf[:0]
	for _, b := range encrypted {
		p.encBuf = append(p.encBuf, b...)
	}
	p.encBuf = p.encBuf[:needed]
	if err := p.cipher.Seal(p.encBuf); err != nil {
		return err
	}
	_, err := p.rw.WriteMsg(p.encBuf)
	return err
}

func (p *messagePipe) writeMsg(ctx *context.T, m message) error {
	return m.write(ctx, p)
}

func (p *messagePipe) readMsg(ctx *context.T) (message, error) {
	msg, err := p.rw.ReadMsg()
	if err != nil {
		return nil, err
	}
	minSize := 2 + p.cipher.MACSize()
	if len(msg) < minSize || !p.cipher.Open(msg) {
		return nil, NewErrInvalidMsg(ctx, invalidType, 0, 0)
	}
	logmsg := msg
	if len(msg) > 128 {
		logmsg = logmsg[:128]
	}
	msgType, msg := msg[0], msg[1:len(msg)-p.cipher.MACSize()]
	var m message
	switch msgType {
	case controlType:
		var msgCmd byte
		msgCmd, msg = msg[0], msg[1:]
		switch msgCmd {
		case setupCmd:
			m = &setup{}
		case tearDownCmd:
			m = &tearDown{}
		case authCmd:
			m = &auth{}
		case openFlowCmd:
			m = &openFlow{}
		case releaseCmd:
			m = &release{}
		default:
			return nil, NewErrUnknownControlMsg(ctx, msgCmd)
		}
	case dataType:
		m = &data{}
	case unencryptedDataType:
		ud := &unencryptedData{}
		payload, err := p.rw.ReadMsg()
		if err != nil {
			return nil, err
		}
		if len(payload) > 0 {
			ud.payload = [][]byte{payload}
		}
		m = ud
	default:
		return nil, NewErrUnknownMsg(ctx, msgType)
	}
	if err = m.read(ctx, msg); err == nil {
		ctx.VI(2).Infof("Read low-level message: %#v", m)
	}
	return m, err
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
