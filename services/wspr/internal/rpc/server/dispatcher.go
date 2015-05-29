// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"sync"

	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/vdlroot/signature"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/wspr/internal/lib"
)

type flowFactory interface {
	createFlow() *Flow
	cleanupFlow(id int32)
}

type invokerFactory interface {
	createInvoker(handle int32, signature []signature.Interface, hasGlobber bool) (rpc.Invoker, error)
}

type authFactory interface {
	createAuthorizer(handle int32, hasAuthorizer bool) (security.Authorizer, error)
}

type dispatcherRequest struct {
	ServerId uint32 `json:"serverId"`
	Suffix   string `json:"suffix"`
}

// dispatcher holds the invoker and the authorizer to be used for lookup.
type dispatcher struct {
	mu                 sync.Mutex
	serverId           uint32
	flowFactory        flowFactory
	invokerFactory     invokerFactory
	authFactory        authFactory
	outstandingLookups map[int32]chan LookupReply
	closed             bool
}

var _ rpc.Dispatcher = (*dispatcher)(nil)

// newDispatcher is a dispatcher factory.
func newDispatcher(serverId uint32, flowFactory flowFactory, invokerFactory invokerFactory, authFactory authFactory) *dispatcher {
	return &dispatcher{
		serverId:           serverId,
		flowFactory:        flowFactory,
		invokerFactory:     invokerFactory,
		authFactory:        authFactory,
		outstandingLookups: make(map[int32]chan LookupReply),
	}
}

func (d *dispatcher) Cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true

	for _, ch := range d.outstandingLookups {
		verr := NewErrServerStopped(nil).(verror.E)
		ch <- LookupReply{Err: &verr}
	}
}

// Lookup implements dispatcher interface Lookup.
func (d *dispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	// If the server has been closed, we immediately return a retryable error.
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil, nil, NewErrServerStopped(nil)
	}
	flow := d.flowFactory.createFlow()
	ch := make(chan LookupReply, 1)
	d.outstandingLookups[flow.ID] = ch
	d.mu.Unlock()

	message := dispatcherRequest{
		ServerId: d.serverId,
		Suffix:   suffix,
	}
	if err := flow.Writer.Send(lib.ResponseDispatcherLookup, message); err != nil {
		verr := verror.Convert(verror.ErrInternal, nil, err).(verror.E)
		ch <- LookupReply{Err: &verr}
	}
	reply := <-ch

	d.mu.Lock()
	delete(d.outstandingLookups, flow.ID)
	d.mu.Unlock()

	d.flowFactory.cleanupFlow(flow.ID)

	if reply.Err != nil {
		return nil, nil, reply.Err
	}
	if reply.Handle < 0 {
		return nil, nil, verror.New(verror.ErrNoExist, nil, "Dispatcher", suffix)
	}

	invoker, err := d.invokerFactory.createInvoker(reply.Handle, reply.Signature, reply.HasGlobber)
	if err != nil {
		return nil, nil, err
	}
	auth, err := d.authFactory.createAuthorizer(reply.Handle, reply.HasAuthorizer)
	if err != nil {
		return nil, nil, err
	}

	return invoker, auth, nil
}

func (d *dispatcher) handleLookupResponse(id int32, data string) {
	d.mu.Lock()
	ch := d.outstandingLookups[id]
	d.mu.Unlock()

	if ch == nil {
		d.flowFactory.cleanupFlow(id)
		vlog.Errorf("unknown invoke request for flow: %d", id)
		return
	}

	var lookupReply LookupReply
	if err := lib.HexVomDecode(data, &lookupReply, nil); err != nil {
		err2 := verror.Convert(verror.ErrInternal, nil, err)
		lookupReply = LookupReply{Err: err2}
		vlog.Errorf("unmarshaling invoke request failed: %v, %s", err, data)
	}

	ch <- lookupReply
}

// StopServing implements dispatcher StopServing.
func (*dispatcher) StopServing() {
}
