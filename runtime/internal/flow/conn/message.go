// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"v.io/v23/context"
	"v.io/v23/flow/message"
	"v.io/x/ref/runtime/internal/rpc/stream/crypto"
)

// TODO(mattr): Consider cleaning up the ControlCipher library to
// eliminate extraneous functionality and reduce copying.
type messagePipe struct {
	rw       MsgReadWriteCloser
	cipher   crypto.ControlCipher
	writeBuf []byte
}

func newMessagePipe(rw MsgReadWriteCloser) *messagePipe {
	return &messagePipe{
		rw:       rw,
		writeBuf: make([]byte, mtu),
		cipher:   &crypto.NullControlCipher{},
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

func (p *messagePipe) writeMsg(ctx *context.T, m message.Message) (err error) {
	// TODO(mattr): Because of the API of the underlying crypto library,
	// an enormous amount of copying happens here.
	// TODO(mattr): We allocate many buffers here to hold potentially
	// many copies of the data.  The maximum memory usage per Conn is probably
	// quite high.  We should try to reduce it.
	if p.writeBuf, err = message.Append(ctx, m, p.writeBuf[:0]); err != nil {
		return err
	}
	if needed := len(p.writeBuf) + p.cipher.MACSize(); cap(p.writeBuf) < needed {
		tmp := make([]byte, needed)
		copy(tmp, p.writeBuf)
		p.writeBuf = tmp
	} else {
		p.writeBuf = p.writeBuf[:needed]
	}
	if err = p.cipher.Seal(p.writeBuf); err != nil {
		return err
	}
	if _, err = p.rw.WriteMsg(p.writeBuf); err == nil {
		ctx.VI(2).Infof("Wrote low-level message: %#v", m)
	}
	return err
}

func (p *messagePipe) readMsg(ctx *context.T) (message.Message, error) {
	msg, err := p.rw.ReadMsg()
	if err != nil {
		return nil, err
	}
	if !p.cipher.Open(msg) {
		return nil, message.NewErrInvalidMsg(ctx, 0, uint64(len(msg)), 0, nil)
	}
	m, err := message.Read(ctx, msg[:len(msg)-p.cipher.MACSize()])
	ctx.VI(2).Infof("Read low-level message: %#v", m)
	return m, nil
}
