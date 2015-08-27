// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package framer

import (
	"io"

	"v.io/v23/flow"
	"v.io/v23/flow/message"
)

// framer is a wrapper of io.ReadWriter that adds framing to a net.Conn
// and implements flow.MsgReadWriteCloser.
type framer struct {
	io.ReadWriteCloser
	buf []byte
}

func New(c io.ReadWriteCloser) flow.MsgReadWriteCloser {
	return &framer{ReadWriteCloser: c}
}

func (f *framer) WriteMsg(data ...[]byte) (int, error) {
	// Compute the message size.
	msgSize := 0
	for _, b := range data {
		msgSize += len(b)
	}
	// Construct a buffer to write that has space for the 3 bytes of framing.
	// If a previous buffer is large enough, reuse it.
	bufSize := msgSize + 3
	if bufSize > len(f.buf) {
		f.buf = make([]byte, bufSize)
	}
	if err := write3ByteUint(f.buf[:3], msgSize); err != nil {
		return 0, err
	}
	head := 3
	for _, b := range data {
		l := len(b)
		copy(f.buf[head:head+l], b)
		head += l
	}
	// Write the buffer to the io.ReadWriter. Remove the frame size
	// from the returned number of bytes written.
	n, err := f.Write(f.buf[:bufSize])
	if err != nil {
		return n - 3, err
	}
	return n - 3, nil
}

func (f *framer) ReadMsg() ([]byte, error) {
	// Read the message size.
	frame := make([]byte, 3)
	if _, err := io.ReadFull(f, frame); err != nil {
		return nil, err
	}
	msgSize := read3ByteUint(frame)
	if msgSize > 0x01ffff {
		// Although it's possible to have messages up to 16MB, we never
		// send messages over about 64kb.  temporarily we're using the
		// arrival of a message > 128kb as a signal that we're talking to
		// an old version of the protocol.  This means we cannot adjust
		// the MTU above 128k until after the transition is over.
		//
		// In the old protocol we send <type><frame high><frame med><frame low>
		// Practically the values can be:
		// <0x00, 0x01, 0x80, 0x81><0x00-0x01><0x00-0xff><0-0xff>
		//
		// In the new protocol we send <frame high><frame med><frame low><type>
		// However, in order to be distinct the frame bytes are actually
		// 0xffffff - size.  Practically the values can be
		// <0xff, 0xfe><0x00-0xff><0x00-0xff><0x00-0x07>.
		//
		// This means if we receive an old protocol message, we should get
		// a very large size.
		// TODO(mattr): Remove this.
		return nil, message.NewErrWrongProtocol(nil)
	}

	// Read the message.
	msg := make([]byte, msgSize)
	if _, err := io.ReadFull(f, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

const maxPacketSize = 0xffffff

func write3ByteUint(dst []byte, n int) error {
	if n > maxPacketSize || n < 0 {
		return NewErrLargerThan3ByteUInt(nil)
	}
	n = maxPacketSize - n
	dst[0] = byte((n & 0xff0000) >> 16)
	dst[1] = byte((n & 0x00ff00) >> 8)
	dst[2] = byte(n & 0x0000ff)
	return nil
}

func read3ByteUint(src []byte) int {
	return maxPacketSize - (int(src[0])<<16 | int(src[1])<<8 | int(src[2]))
}
