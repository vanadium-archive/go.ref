// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"v.io/v23/context"
	"v.io/v23/flow"
	"v.io/v23/security"

	"v.io/x/ref/runtime/internal/flow/flowcontrol"
	"v.io/x/ref/runtime/internal/lib/upcqueue"
)

type flw struct {
	id               flowID
	ctx              *context.T
	conn             *Conn
	closed           chan struct{}
	worker           *flowcontrol.Worker
	opened           bool
	q                *upcqueue.T
	readBufs         [][]byte
	dialerBlessings  security.Blessings
	dialerDischarges map[string]security.Discharge
}

var _ flow.Flow = &flw{}

func (c *Conn) newFlowLocked(ctx *context.T, id flowID) *flw {
	f := &flw{
		id:     id,
		ctx:    ctx,
		conn:   c,
		closed: make(chan struct{}),
		worker: c.fc.NewWorker(flowPriority),
		q:      upcqueue.New(),
	}
	c.flows[id] = f
	return f
}

func (f *flw) dialed() bool {
	return f.id%2 == 0
}

// Implement io.Reader.
// Read and ReadMsg should not be called concurrently with themselves
// or each other.
func (f *flw) Read(p []byte) (n int, err error) {
	for {
		for len(f.readBufs) > 0 && len(f.readBufs[0]) == 0 {
			f.readBufs = f.readBufs[1:]
		}
		if len(f.readBufs) > 0 {
			break
		}
		var msg interface{}
		msg, err = f.q.Get(f.ctx.Done())
		f.readBufs = msg.([][]byte)
	}
	n = copy(p, f.readBufs[0])
	f.readBufs[0] = f.readBufs[0][n:]
	return
}

// ReadMsg is like read, but it reads bytes in chunks.  Depending on the
// implementation the batch boundaries might or might not be significant.
// Read and ReadMsg should not be called concurrently with themselves
// or each other.
func (f *flw) ReadMsg() (buf []byte, err error) {
	for {
		for len(f.readBufs) > 0 {
			buf, f.readBufs = f.readBufs[0], f.readBufs[1:]
			if len(buf) > 0 {
				return buf, nil
			}
		}
		bufs, err := f.q.Get(f.ctx.Done())
		if err != nil {
			return nil, err
		}
		f.readBufs = bufs.([][]byte)
	}
}

// Implement io.Writer.
// Write, WriteMsg, and WriteMsgAndClose should not be called concurrently
// with themselves or each other.
func (f *flw) Write(p []byte) (n int, err error) {
	return f.WriteMsg(p)
}

func (f *flw) writeMsg(alsoClose bool, parts ...[]byte) (int, error) {
	sent := 0
	var left []byte

	f.ctx.VI(3).Infof("trying to write: %d.", f.id)
	err := f.worker.Run(f.ctx, func(tokens int) (int, bool, error) {
		f.ctx.VI(3).Infof("writing: %d.", f.id)
		if !f.opened {
			// TODO(mattr): we should be able to send multiple messages
			// in a single writeMsg call.
			err := f.conn.mp.writeMsg(f.ctx, &openFlow{
				id:              f.id,
				initialCounters: defaultBufferSize,
			})
			if err != nil {
				return 0, false, err
			}
			f.opened = true
		}
		size := 0
		var bufs [][]byte
		if len(left) > 0 {
			size += len(left)
			bufs = append(bufs, left)
		}
		for size <= tokens && len(parts) > 0 {
			bufs = append(bufs, parts[0])
			size += len(parts[0])
			parts = parts[1:]
		}
		if size > tokens {
			lidx := len(bufs) - 1
			last := bufs[lidx]
			take := len(last) - (size - tokens)
			bufs[lidx] = last[:take]
			left = last[take:]
		}
		d := &data{
			id:      f.id,
			payload: bufs,
		}
		done := len(left) == 0 && len(parts) == 0
		if alsoClose && done {
			d.flags |= closeFlag
		}
		sent += size
		return size, done, f.conn.mp.writeMsg(f.ctx, d)
	})
	return sent, err
}

// WriteMsg is like Write, but allows writing more than one buffer at a time.
// The data in each buffer is written sequentially onto the flow.  Returns the
// number of bytes written.  WriteMsg must return a non-nil error if it writes
// less than the total number of bytes from all buffers.
// Write, WriteMsg, and WriteMsgAndClose should not be called concurrently
// with themselves or each other.
func (f *flw) WriteMsg(parts ...[]byte) (int, error) {
	return f.writeMsg(false, parts...)
}

// WriteMsgAndClose performs WriteMsg and then closes the flow.
// Write, WriteMsg, and WriteMsgAndClose should not be called concurrently
// with themselves or each other.
func (f *flw) WriteMsgAndClose(parts ...[]byte) (int, error) {
	return f.writeMsg(true, parts...)
}

// SetContext sets the context associated with the flow.  Typically this is
// used to set state that is only available after the flow is connected, such
// as a more restricted flow timeout, or the language of the request.
//
// The flow.Manager associated with ctx must be the same flow.Manager that the
// flow was dialed or accepted from, otherwise an error is returned.
// TODO(mattr): enforce this restriction.
//
// TODO(mattr): update v23/flow documentation.
// SetContext may not be called concurrently with other methods.
func (f *flw) SetContext(ctx *context.T) error {
	f.ctx = ctx
	return nil
}

// LocalBlessings returns the blessings presented by the local end of the flow
// during authentication.
func (f *flw) LocalBlessings() security.Blessings {
	if f.dialed() {
		return f.dialerBlessings
	}
	return f.conn.AcceptorBlessings()
}

// RemoteBlessings returns the blessings presented by the remote end of the
// flow during authentication.
func (f *flw) RemoteBlessings() security.Blessings {
	if f.dialed() {
		return f.conn.AcceptorBlessings()
	}
	return f.dialerBlessings
}

// LocalDischarges returns the discharges presented by the local end of the
// flow during authentication.
//
// Discharges are organized in a map keyed by the discharge-identifier.
func (f *flw) LocalDischarges() map[string]security.Discharge {
	if f.dialed() {
		return f.dialerDischarges
	}
	return f.conn.AcceptorDischarges()
}

// RemoteDischarges returns the discharges presented by the remote end of the
// flow during authentication.
//
// Discharges are organized in a map keyed by the discharge-identifier.
func (f *flw) RemoteDischarges() map[string]security.Discharge {
	if f.dialed() {
		return f.conn.AcceptorDischarges()
	}
	return f.dialerDischarges
}

// Conn returns the connection the flow is multiplexed on.
func (f *flw) Conn() flow.Conn {
	return f.conn
}

// Closed returns a channel that remains open until the flow has been closed or
// the ctx to the Dial or Accept call used to create the flow has been cancelled.
func (f *flw) Closed() <-chan struct{} {
	return f.closed
}
