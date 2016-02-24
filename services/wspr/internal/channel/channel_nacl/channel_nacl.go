// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package channel_nacl

import (
	"bytes"
	"fmt"
	"runtime/ppapi"

	"v.io/v23/vom"
	"v.io/x/ref/services/wspr/internal/channel" // contains most of the logic, factored out for testing
)

type Channel struct {
	impl      *channel.Channel
	ppapiInst ppapi.Instance
}

func sendMessageToBrowser(ppapiInst ppapi.Instance, m channel.Message) {
	var outBuf bytes.Buffer
	enc := vom.NewEncoder(&outBuf)
	if err := enc.Encode(m); err != nil {
		panic(fmt.Sprintf("Error encoding message %v: %v", m, err))
	}
	outVar := ppapi.VarFromByteSlice(outBuf.Bytes())
	ppapiInst.PostMessage(outVar)
}

func NewChannel(ppapiInst ppapi.Instance) *Channel {
	sendMessageFunc := func(m channel.Message) {
		sendMessageToBrowser(ppapiInst, m)
	}
	return &Channel{
		impl:      channel.NewChannel(sendMessageFunc),
		ppapiInst: ppapiInst,
	}
}

func (c *Channel) RegisterRequestHandler(typ string, handler channel.RequestHandler) {
	c.impl.RegisterRequestHandler(typ, handler)
}

func (c *Channel) PerformRpc(typ string, body *vom.RawBytes) (*vom.RawBytes, error) {
	return c.impl.PerformRpc(typ, body)
}

func (c *Channel) HandleMessage(v ppapi.Var) {
	// Read input message
	b, err := v.AsByteSlice()
	if err != nil {
		panic(fmt.Sprintf("Cannot convert message to byte slice: %v", err))
	}

	buf := bytes.NewBuffer(b)
	dec := vom.NewDecoder(buf)
	var m channel.Message
	if err := dec.Decode(&m); err != nil {
		panic(fmt.Sprintf("Error decoding message: %v", err))
	}

	c.impl.HandleMessage(m)
}
