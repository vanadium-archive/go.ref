// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package app

import (
	"fmt"
	"time"

	"v.io/v23/context"
	"v.io/v23/security"
	"v.io/x/ref/services/wspr/internal/lib"
	"v.io/x/ref/services/wspr/internal/rpc/server"
)

// This is a Granter that redirects grant requests to javascript
// and waits for the response.
// Implements security.Granter
type jsGranter struct {
	c             server.ServerHelper
	granterHandle GranterHandle
}

func (g *jsGranter) Grant(ctx *context.T, call security.Call) (blessings security.Blessings, err error) {
	stream := &granterStream{make(chan *GranterResponse, 1)}
	flow := g.c.CreateNewFlow(stream, stream)
	request := &GranterRequest{
		GranterHandle: g.granterHandle,
		Call:          server.ConvertSecurityCall(g.c, ctx, call, true),
	}
	encoded, err := lib.HexVomEncode(request, nil)
	if err != nil {
		return security.Blessings{}, err
	}
	if err := flow.Writer.Send(lib.ResponseGranterRequest, encoded); err != nil {
		return security.Blessings{}, err
	}
	timeoutTime := time.Second * 5 // get real timeout
	select {
	case <-time.After(timeoutTime):
		return security.Blessings{}, fmt.Errorf("Timed out receiving response from javascript granter")
	case response := <-stream.c:
		if response.Err != nil {
			return security.Blessings{}, response.Err
		}
		return response.Blessings, nil
	}
}

func (g *jsGranter) RPCCallOpt() {}

// Granter stream exists because our incoming request handling mechanism
// works on streams.
// It simply decodes the response from js and sends it to the granter.
type granterStream struct {
	c chan *GranterResponse
}

func (g *granterStream) Send(item interface{}) error {
	dataString := item.(string)
	var gr *GranterResponse
	if err := lib.HexVomDecode(dataString, &gr, nil); err != nil {
		return fmt.Errorf("error decoding granter response: %v", err)
	}
	g.c <- gr
	return nil
}

func (g *granterStream) Recv(itemptr interface{}) error {
	panic("Shouldn't be called")
}

func (g *granterStream) CloseSend() error {
	close(g.c)
	return nil
}
