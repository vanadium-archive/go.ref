// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build mojo

// Implementation of Syncbase Mojo stubs. Our strategy is to translate Mojo stub
// requests into Vanadium stub requests, and Vanadium stub responses into Mojo
// stub responses. As part of this procedure, we synthesize "fake" ctx and call
// objects to pass to the Vanadium stubs.

package server

import (
	"bytes"
	"fmt"
	"strings"

	"mojo/public/go/bindings"

	mojom "mojom/syncbase"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	"v.io/v23/services/permissions"
	wire "v.io/v23/services/syncbase"
	nosqlwire "v.io/v23/services/syncbase/nosql"
	"v.io/v23/verror"
	"v.io/v23/vtrace"
)

const NoSchema int32 = -1

type mojoImpl struct {
	ctx  *context.T
	srv  rpc.Server
	disp rpc.Dispatcher
}

func NewMojoImpl(ctx *context.T, srv rpc.Server, disp rpc.Dispatcher) *mojoImpl {
	return &mojoImpl{ctx: ctx, srv: srv, disp: disp}
}

func methodDesc(desc rpc.InterfaceDesc, name string) rpc.MethodDesc {
	for _, method := range desc.Methods {
		if method.Name == name {
			return method
		}
	}
	panic(fmt.Sprintf("unknown method: %s.%s", desc.Name, name))
}

func (m *mojoImpl) newCtxCall(suffix string, method rpc.MethodDesc) (*context.T, rpc.ServerCall) {
	ctx, _ := vtrace.WithNewTrace(m.ctx)
	return ctx, newMojoServerCall(ctx, m.srv, suffix, method)
}

////////////////////////////////////////
// Struct converters

func toMojoError(err error) mojom.Error {
	if err == nil {
		return mojom.Error{}
	}
	return mojom.Error{
		Id:         string(verror.ErrorID(err)),
		ActionCode: uint32(verror.Action(err)),
		Msg:        err.Error(),
	}
}

func toV23Perms(mPerms mojom.Perms) (access.Permissions, error) {
	return access.ReadPermissions(strings.NewReader(mPerms.Json))
}

func toMojoPerms(vPerms access.Permissions) (mojom.Perms, error) {
	b := new(bytes.Buffer)
	if err := access.WritePermissions(b, vPerms); err != nil {
		return mojom.Perms{}, err
	}
	return mojom.Perms{Json: b.String()}, nil
}

////////////////////////////////////////
// Stub getters

func (m *mojoImpl) lookupAndAuthorize(ctx *context.T, call rpc.ServerCall, suffix string) (interface{}, error) {
	resInt, auth, err := m.disp.Lookup(ctx, suffix)
	if err != nil {
		return nil, err
	}
	if err := auth.Authorize(ctx, call.Security()); err != nil {
		return nil, verror.New(verror.ErrNoAccess, ctx, err)
	}
	return resInt, nil
}

func (m *mojoImpl) getService(ctx *context.T, call rpc.ServerCall) (wire.ServiceServerStubMethods, error) {
	resInt, err := m.lookupAndAuthorize(ctx, call, "")
	if err != nil {
		return nil, err
	}
	if res, ok := resInt.(wire.ServiceServerStubMethods); !ok {
		return nil, verror.NewErrInternal(ctx)
	} else {
		return res, nil
	}
}

func (m *mojoImpl) getApp(ctx *context.T, call rpc.ServerCall, name string) (wire.AppServerStubMethods, error) {
	resInt, err := m.lookupAndAuthorize(ctx, call, name)
	if err != nil {
		return nil, err
	}
	if res, ok := resInt.(wire.AppServerStubMethods); !ok {
		return nil, verror.NewErrInternal(ctx)
	} else {
		return res, nil
	}
}

func (m *mojoImpl) getDb(ctx *context.T, call rpc.ServerCall, name string) (nosqlwire.DatabaseServerStubMethods, error) {
	resInt, err := m.lookupAndAuthorize(ctx, call, name)
	if err != nil {
		return nil, err
	}
	if res, ok := resInt.(nosqlwire.DatabaseServerStubMethods); !ok {
		return nil, verror.NewErrInternal(ctx)
	} else {
		return res, nil
	}
}

func (m *mojoImpl) getTable(ctx *context.T, call rpc.ServerCall, name string) (nosqlwire.TableServerStubMethods, error) {
	resInt, err := m.lookupAndAuthorize(ctx, call, name)
	if err != nil {
		return nil, err
	}
	if res, ok := resInt.(nosqlwire.TableServerStubMethods); !ok {
		return nil, verror.NewErrInternal(ctx)
	} else {
		return res, nil
	}
}

func (m *mojoImpl) getRow(ctx *context.T, call rpc.ServerCall, name string) (nosqlwire.RowServerStubMethods, error) {
	resInt, err := m.lookupAndAuthorize(ctx, call, name)
	if err != nil {
		return nil, err
	}
	if res, ok := resInt.(nosqlwire.RowServerStubMethods); !ok {
		return nil, verror.NewErrInternal(ctx)
	} else {
		return res, nil
	}
}

////////////////////////////////////////
// Service

// TODO(sadovsky): All stub implementations return a nil error (the last return
// value), since that error doesn't make it back to the IPC client. Chat with
// rogulenko@ about whether we should change the Go Mojo stub generator to drop
// these errors.
func (m *mojoImpl) ServiceGetPermissions() (mojom.Error, mojom.Perms, string, error) {
	ctx, call := m.newCtxCall("", methodDesc(permissions.ObjectDesc, "GetPermissions"))
	stub, err := m.getService(ctx, call)
	if err != nil {
		return toMojoError(err), mojom.Perms{}, "", nil
	}
	vPerms, version, err := stub.GetPermissions(ctx, call)
	if err != nil {
		return toMojoError(err), mojom.Perms{}, "", nil
	}
	mPerms, err := toMojoPerms(vPerms)
	if err != nil {
		return toMojoError(err), mojom.Perms{}, "", nil
	}
	return toMojoError(err), mPerms, version, nil
}

func (m *mojoImpl) ServiceSetPermissions(mPerms mojom.Perms, version string) (mojom.Error, error) {
	ctx, call := m.newCtxCall("", methodDesc(permissions.ObjectDesc, "SetPermissions"))
	stub, err := m.getService(ctx, call)
	if err != nil {
		return toMojoError(err), nil
	}
	vPerms, err := toV23Perms(mPerms)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.SetPermissions(ctx, call, vPerms, version)
	return toMojoError(err), nil
}

////////////////////////////////////////
// App

func (m *mojoImpl) AppCreate(name string, mPerms mojom.Perms) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(wire.AppDesc, "Create"))
	stub, err := m.getApp(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	vPerms, err := toV23Perms(mPerms)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.Create(ctx, call, vPerms)
	return toMojoError(err), nil
}

func (m *mojoImpl) AppDestroy(name string) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(wire.AppDesc, "Destroy"))
	stub, err := m.getApp(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.Destroy(ctx, call)
	return toMojoError(err), nil
}

func (m *mojoImpl) AppExists(name string) (mojom.Error, bool, error) {
	ctx, call := m.newCtxCall(name, methodDesc(wire.AppDesc, "Exists"))
	stub, err := m.getApp(ctx, call, name)
	if err != nil {
		return toMojoError(err), false, nil
	}
	exists, err := stub.Exists(ctx, call)
	return toMojoError(err), exists, nil
}

func (m *mojoImpl) AppGetPermissions(name string) (mojom.Error, mojom.Perms, string, error) {
	ctx, call := m.newCtxCall(name, methodDesc(permissions.ObjectDesc, "GetPermissions"))
	stub, err := m.getApp(ctx, call, name)
	if err != nil {
		return toMojoError(err), mojom.Perms{}, "", nil
	}
	vPerms, version, err := stub.GetPermissions(ctx, call)
	if err != nil {
		return toMojoError(err), mojom.Perms{}, "", nil
	}
	mPerms, err := toMojoPerms(vPerms)
	if err != nil {
		return toMojoError(err), mojom.Perms{}, "", nil
	}
	return toMojoError(err), mPerms, version, nil
}

func (m *mojoImpl) AppSetPermissions(name string, mPerms mojom.Perms, version string) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(permissions.ObjectDesc, "SetPermissions"))
	stub, err := m.getApp(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	vPerms, err := toV23Perms(mPerms)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.SetPermissions(ctx, call, vPerms, version)
	return toMojoError(err), nil
}

////////////////////////////////////////
// nosql.Database

func (m *mojoImpl) DbCreate(name string, mPerms mojom.Perms) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.DatabaseDesc, "Create"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	vPerms, err := toV23Perms(mPerms)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.Create(ctx, call, nil, vPerms)
	return toMojoError(err), nil
}

func (m *mojoImpl) DbDestroy(name string) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.DatabaseDesc, "Destroy"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.Destroy(ctx, call, NoSchema)
	return toMojoError(err), nil
}

func (m *mojoImpl) DbExists(name string) (mojom.Error, bool, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.DatabaseDesc, "Exists"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), false, nil
	}
	exists, err := stub.Exists(ctx, call, NoSchema)
	return toMojoError(err), exists, nil
}

func (m *mojoImpl) DbExec(name string, query string, stream mojom.ExecStream_Pointer) (mojom.Error, error) {
	return mojom.Error{}, nil
}

func (m *mojoImpl) DbBeginBatch(name string, bo *mojom.BatchOptions) (mojom.Error, string, error) {
	return mojom.Error{}, "", nil
}

func (m *mojoImpl) DbCommit(name string) (mojom.Error, error) {
	return mojom.Error{}, nil
}

func (m *mojoImpl) DbAbort(name string) (mojom.Error, error) {
	return mojom.Error{}, nil
}

func (m *mojoImpl) DbGetPermissions(name string) (mojom.Error, mojom.Perms, string, error) {
	ctx, call := m.newCtxCall(name, methodDesc(permissions.ObjectDesc, "GetPermissions"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), mojom.Perms{}, "", nil
	}
	vPerms, version, err := stub.GetPermissions(ctx, call)
	if err != nil {
		return toMojoError(err), mojom.Perms{}, "", nil
	}
	mPerms, err := toMojoPerms(vPerms)
	if err != nil {
		return toMojoError(err), mojom.Perms{}, "", nil
	}
	return toMojoError(err), mPerms, version, nil
}

func (m *mojoImpl) DbSetPermissions(name string, mPerms mojom.Perms, version string) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(permissions.ObjectDesc, "SetPermissions"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	vPerms, err := toV23Perms(mPerms)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.SetPermissions(ctx, call, vPerms, version)
	return toMojoError(err), nil
}

////////////////////////////////////////
// nosql.Database:SyncGroupManager

func (m *mojoImpl) DbGetSyncGroupNames(name string) (mojom.Error, []string, error) {
	return mojom.Error{}, nil, nil
}

func (m *mojoImpl) DbCreateSyncGroup(name, sgName string, spec mojom.SyncGroupSpec, myInfo mojom.SyncGroupMemberInfo) (mojom.Error, error) {
	return mojom.Error{}, nil
}

func (m *mojoImpl) DbJoinSyncGroup(name, sgName string, myInfo mojom.SyncGroupMemberInfo) (mojom.Error, error) {
	return mojom.Error{}, nil
}

func (m *mojoImpl) DbLeaveSyncGroup(name, sgName string) (mojom.Error, error) {
	return mojom.Error{}, nil
}

func (m *mojoImpl) DbDestroySyncGroup(name, sgName string) (mojom.Error, error) {
	return mojom.Error{}, nil
}

func (m *mojoImpl) DbEjectFromSyncGroup(name, sgName string, member string) (mojom.Error, error) {
	return mojom.Error{}, nil
}

func (m *mojoImpl) DbGetSyncGroupSpec(name, sgName string) (mojom.Error, mojom.SyncGroupSpec, string, error) {
	return mojom.Error{}, mojom.SyncGroupSpec{}, "", nil
}

func (m *mojoImpl) DbSetSyncGroupSpec(name, sgName string, spec mojom.SyncGroupSpec, version string) (mojom.Error, error) {
	return mojom.Error{}, nil
}

func (m *mojoImpl) DbGetSyncGroupMembers(name, sgName string) (mojom.Error, map[string]mojom.SyncGroupMemberInfo, error) {
	return mojom.Error{}, nil, nil
}

////////////////////////////////////////
// nosql.Table

func (m *mojoImpl) TableCreate(name string, mPerms mojom.Perms) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.TableDesc, "Create"))
	stub, err := m.getTable(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	vPerms, err := toV23Perms(mPerms)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.Create(ctx, call, NoSchema, vPerms)
	return toMojoError(err), nil
}

func (m *mojoImpl) TableDestroy(name string) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.TableDesc, "Destroy"))
	stub, err := m.getTable(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.Destroy(ctx, call, NoSchema)
	return toMojoError(err), nil
}

func (m *mojoImpl) TableExists(name string) (mojom.Error, bool, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.TableDesc, "Exists"))
	stub, err := m.getTable(ctx, call, name)
	if err != nil {
		return toMojoError(err), false, nil
	}
	exists, err := stub.Exists(ctx, call, NoSchema)
	return toMojoError(err), exists, nil
}

func (m *mojoImpl) TableDeleteRange(name string, start, limit []byte) (mojom.Error, error) {
	return mojom.Error{}, nil
}

type scanStreamImpl struct {
	ctx   *context.T
	proxy *mojom.ScanStream_Proxy
}

func (s *scanStreamImpl) Send(item interface{}) error {
	kv, ok := item.(nosqlwire.KeyValue)
	if !ok {
		return verror.NewErrInternal(s.ctx)
	}

	return s.proxy.OnKeyValue(mojom.KeyValue{
		Key:   kv.Key,
		Value: kv.Value,
	})
}

func (s *scanStreamImpl) Recv(_ interface{}) error {
	// This should never be called.
	return verror.NewErrInternal(s.ctx)
}

var _ rpc.Stream = (*scanStreamImpl)(nil)

// TODO(nlacasse): Provide some way for the client to cancel the stream.
func (m *mojoImpl) TableScan(name string, start, limit []byte, ptr mojom.ScanStream_Pointer) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.TableDesc, "Scan"))
	stub, err := m.getTable(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}

	proxy := mojom.NewScanStreamProxy(ptr, bindings.GetAsyncWaiter())

	tableScanServerCallStub := &nosqlwire.TableScanServerCallStub{struct {
		rpc.Stream
		rpc.ServerCall
	}{
		&scanStreamImpl{
			ctx:   ctx,
			proxy: proxy,
		},
		call,
	}}

	err = stub.Scan(ctx, tableScanServerCallStub, NoSchema, start, limit)

	// NOTE(nlacasse): Since we are already streaming, we send any error back
	// to the client on the stream.  The TableScan function itself should not
	// return an error at this point.
	proxy.OnDone(toMojoError(err))
	return mojom.Error{}, nil
}

func (m *mojoImpl) TableGetPermissions(name, key string) (mojom.Error, []mojom.PrefixPerms, error) {
	return mojom.Error{}, nil, nil
}

func (m *mojoImpl) TableSetPermissions(name, prefix string, mPerms mojom.Perms) (mojom.Error, error) {
	return mojom.Error{}, nil
}

func (m *mojoImpl) TableDeletePermissions(name, prefix string) (mojom.Error, error) {
	return mojom.Error{}, nil
}

////////////////////////////////////////
// nosql.Row

func (m *mojoImpl) RowExists(name string) (mojom.Error, bool, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.RowDesc, "Exists"))
	stub, err := m.getRow(ctx, call, name)
	if err != nil {
		return toMojoError(err), false, nil
	}
	exists, err := stub.Exists(ctx, call, NoSchema)
	return toMojoError(err), exists, nil
}

func (m *mojoImpl) RowGet(name string) (mojom.Error, []byte, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.RowDesc, "Get"))
	stub, err := m.getRow(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil, nil
	}
	value, err := stub.Get(ctx, call, NoSchema)
	return toMojoError(err), value, nil
}

func (m *mojoImpl) RowPut(name string, value []byte) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.RowDesc, "Put"))
	stub, err := m.getRow(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.Put(ctx, call, NoSchema, value)
	return toMojoError(err), nil
}

func (m *mojoImpl) RowDelete(name string) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.RowDesc, "Delete"))
	stub, err := m.getRow(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.Delete(ctx, call, NoSchema)
	return toMojoError(err), nil
}
