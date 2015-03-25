// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package impl

import (
	"v.io/v23/rpc"
	svtrace "v.io/v23/services/mgmt/vtrace"
	"v.io/v23/uniqueid"
	"v.io/v23/verror"
	"v.io/v23/vtrace"
)

type vtraceService struct{}

func (v *vtraceService) Trace(call rpc.ServerCall, id uniqueid.Id) (vtrace.TraceRecord, error) {
	store := vtrace.GetStore(call.Context())
	tr := store.TraceRecord(id)
	if tr == nil {
		return vtrace.TraceRecord{}, verror.New(verror.ErrNoExist, call.Context(), "No trace with id %x", id)
	}
	return *tr, nil
}

func (v *vtraceService) AllTraces(call svtrace.StoreAllTracesServerCall) error {
	// TODO(mattr): Consider changing the store to allow us to iterate through traces
	// when there are many.
	store := vtrace.GetStore(call.Context())
	traces := store.TraceRecords()
	for i := range traces {
		if err := call.SendStream().Send(traces[i]); err != nil {
			return err
		}
	}
	return nil
}

func NewVtraceService() interface{} {
	return svtrace.StoreServer(&vtraceService{})
}
