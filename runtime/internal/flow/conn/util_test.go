// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package conn

import (
	"v.io/v23/context"
	"v.io/v23/flow"
)

type mRW struct {
	recieve <-chan []byte
	send    chan<- []byte
	ctx     *context.T
}

func newMRWPair(ctx *context.T) (flow.MsgReadWriter, flow.MsgReadWriter) {
	ac, bc := make(chan []byte), make(chan []byte)
	a := &mRW{recieve: ac, send: bc, ctx: ctx}
	b := &mRW{recieve: bc, send: ac, ctx: ctx}
	return a, b
}

func (f *mRW) WriteMsg(data ...[]byte) (int, error) {
	buf := []byte{}
	for _, d := range data {
		buf = append(buf, d...)
	}
	f.send <- buf
	f.ctx.VI(5).Infof("Wrote: %v", buf)
	return len(buf), nil
}
func (f *mRW) ReadMsg() (buf []byte, err error) {
	buf = <-f.recieve
	f.ctx.VI(5).Infof("Read: %v", buf)
	return buf, nil
}

type fh chan<- flow.Flow

func (fh fh) HandleFlow(f flow.Flow) error {
	if fh == nil {
		panic("writing to nil flow handler")
	}
	fh <- f
	return nil
}
