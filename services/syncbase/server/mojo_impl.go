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
	watchwire "v.io/v23/services/watch"
	"v.io/v23/syncbase/nosql"
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

func toV23SyncGroupMemberInfo(mInfo mojom.SyncGroupMemberInfo) nosqlwire.SyncGroupMemberInfo {
	return nosqlwire.SyncGroupMemberInfo{
		SyncPriority: mInfo.SyncPriority,
	}
}

func toMojoSyncGroupMemberInfo(vInfo nosqlwire.SyncGroupMemberInfo) mojom.SyncGroupMemberInfo {
	return mojom.SyncGroupMemberInfo{
		SyncPriority: vInfo.SyncPriority,
	}
}

func toV23SyncGroupSpec(mSpec mojom.SyncGroupSpec) (nosqlwire.SyncGroupSpec, error) {
	v23Perms, err := toV23Perms(mSpec.Perms)
	if err != nil {
		return nosqlwire.SyncGroupSpec{}, err
	}
	prefixes := make([]nosqlwire.SyncGroupPrefix, len(mSpec.Prefixes))
	for i, v := range mSpec.Prefixes {
		prefixes[i].TableName = v.TableName
		prefixes[i].RowPrefix = v.RowPrefix
	}
	return nosqlwire.SyncGroupSpec{
		Description: mSpec.Description,
		Perms:       v23Perms,
		Prefixes:    prefixes,
		MountTables: mSpec.MountTables,
		IsPrivate:   mSpec.IsPrivate,
	}, nil
}

func toMojoSyncGroupSpec(vSpec nosqlwire.SyncGroupSpec) (mojom.SyncGroupSpec, error) {
	mPerms, err := toMojoPerms(vSpec.Perms)
	if err != nil {
		return mojom.SyncGroupSpec{}, err
	}
	prefixes := make([]mojom.SyncGroupPrefix, len(vSpec.Prefixes))
	for i, v := range vSpec.Prefixes {
		prefixes[i].TableName = v.TableName
		prefixes[i].RowPrefix = v.RowPrefix
	}
	return mojom.SyncGroupSpec{
		Description: vSpec.Description,
		Perms:       mPerms,
		Prefixes:    prefixes,
		MountTables: vSpec.MountTables,
		IsPrivate:   vSpec.IsPrivate,
	}, nil
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

type watchGlobStreamImpl struct {
	ctx   *context.T
	proxy *mojom.WatchGlobStream_Proxy
}

func (s *watchGlobStreamImpl) Send(item interface{}) error {
	c, ok := item.(watchwire.Change)
	if !ok {
		return verror.NewErrInternal(s.ctx)
	}

	vc := nosql.ToWatchChange(c)
	mc := mojom.WatchChange{
		TableName:    vc.Table,
		RowKey:       vc.Row,
		ChangeType:   uint32(vc.ChangeType),
		ValueBytes:   vc.ValueBytes,
		ResumeMarker: vc.ResumeMarker,
		FromSync:     vc.FromSync,
		Continued:    vc.Continued,
	}

	// proxy.OnChange() blocks until the client acks the previous invocation,
	// thus providing flow control.
	return s.proxy.OnChange(mc)
}

func (s *watchGlobStreamImpl) Recv(_ interface{}) error {
	// This should never be called.
	return verror.NewErrInternal(s.ctx)
}

var _ rpc.Stream = (*watchGlobStreamImpl)(nil)

func (m *mojoImpl) DbWatchGlob(name string, mReq mojom.GlobRequest, ptr mojom.WatchGlobStream_Pointer) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(watchwire.GlobWatcherDesc, "WatchGlob"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}

	var vReq = watchwire.GlobRequest{
		Pattern:      mReq.Pattern,
		ResumeMarker: watchwire.ResumeMarker(mReq.ResumeMarker),
	}
	proxy := mojom.NewWatchGlobStreamProxy(ptr, bindings.GetAsyncWaiter())

	watchGlobServerCallStub := &watchwire.GlobWatcherWatchGlobServerCallStub{struct {
		rpc.Stream
		rpc.ServerCall
	}{
		&watchGlobStreamImpl{
			ctx:   ctx,
			proxy: proxy,
		},
		call,
	}}

	go func() {
		var err = stub.WatchGlob(ctx, watchGlobServerCallStub, vReq)
		// NOTE(nlacasse): Since we are already streaming, we send any error back
		// to the client on the stream.  The WatchGlob function itself should not
		// return an error at this point.
		// NOTE(aghassemi): WatchGlob call is long-running and does not return
		// unless there is an error.
		proxy.OnError(toMojoError(err))
	}()

	return mojom.Error{}, nil
}

func (m *mojoImpl) DbGetResumeMarker(name string) (mojom.Error, []byte, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.DatabaseWatcherDesc, "GetResumeMarker"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil, nil
	}
	marker, err := stub.GetResumeMarker(ctx, call)
	return toMojoError(err), marker, nil
}

////////////////////////////////////////
// nosql.Database:SyncGroupManager

func (m *mojoImpl) DbGetSyncGroupNames(name string) (mojom.Error, []string, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.SyncGroupManagerDesc, "GetSyncGroupNames"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil, nil
	}
	names, err := stub.GetSyncGroupNames(ctx, call)
	return toMojoError(err), names, nil
}

func (m *mojoImpl) DbCreateSyncGroup(name, sgName string, spec mojom.SyncGroupSpec, myInfo mojom.SyncGroupMemberInfo) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.SyncGroupManagerDesc, "CreateSyncGroup"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	v23Spec, err := toV23SyncGroupSpec(spec)
	if err != nil {
		return toMojoError(err), nil
	}
	return toMojoError(stub.CreateSyncGroup(ctx, call, sgName, v23Spec, toV23SyncGroupMemberInfo(myInfo))), nil
}

func (m *mojoImpl) DbJoinSyncGroup(name, sgName string, myInfo mojom.SyncGroupMemberInfo) (mojom.Error, mojom.SyncGroupSpec, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.SyncGroupManagerDesc, "JoinSyncGroup"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), mojom.SyncGroupSpec{}, nil
	}
	spec, err := stub.JoinSyncGroup(ctx, call, sgName, toV23SyncGroupMemberInfo(myInfo))
	if err != nil {
		return toMojoError(err), mojom.SyncGroupSpec{}, nil
	}
	mojoSpec, err := toMojoSyncGroupSpec(spec)
	if err != nil {
		return toMojoError(err), mojom.SyncGroupSpec{}, nil
	}
	return toMojoError(err), mojoSpec, nil
}

func (m *mojoImpl) DbLeaveSyncGroup(name, sgName string) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.SyncGroupManagerDesc, "LeaveSyncGroup"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	return toMojoError(stub.LeaveSyncGroup(ctx, call, sgName)), nil
}

func (m *mojoImpl) DbDestroySyncGroup(name, sgName string) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.SyncGroupManagerDesc, "DestroySyncGroup"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	return toMojoError(stub.DestroySyncGroup(ctx, call, sgName)), nil
}

func (m *mojoImpl) DbEjectFromSyncGroup(name, sgName string, member string) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.SyncGroupManagerDesc, "EjectFromSyncGroup"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	return toMojoError(stub.EjectFromSyncGroup(ctx, call, sgName, member)), nil
}

func (m *mojoImpl) DbGetSyncGroupSpec(name, sgName string) (mojom.Error, mojom.SyncGroupSpec, string, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.SyncGroupManagerDesc, "GetSyncGroupSpec"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), mojom.SyncGroupSpec{}, "", nil
	}
	spec, version, err := stub.GetSyncGroupSpec(ctx, call, sgName)
	mojoSpec, err := toMojoSyncGroupSpec(spec)
	if err != nil {
		return toMojoError(err), mojom.SyncGroupSpec{}, "", nil
	}
	return toMojoError(err), mojoSpec, version, nil
}

func (m *mojoImpl) DbSetSyncGroupSpec(name, sgName string, spec mojom.SyncGroupSpec, version string) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.SyncGroupManagerDesc, "SetSyncGroupSpec"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	v23Spec, err := toV23SyncGroupSpec(spec)
	if err != nil {
		return toMojoError(err), nil
	}
	return toMojoError(stub.SetSyncGroupSpec(ctx, call, sgName, v23Spec, version)), nil
}

func (m *mojoImpl) DbGetSyncGroupMembers(name, sgName string) (mojom.Error, map[string]mojom.SyncGroupMemberInfo, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.SyncGroupManagerDesc, "GetSyncGroupMembers"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil, nil
	}
	members, err := stub.GetSyncGroupMembers(ctx, call, sgName)
	if err != nil {
		return toMojoError(err), nil, nil
	}
	mojoMembers := make(map[string]mojom.SyncGroupMemberInfo, len(members))
	for name, member := range members {
		mojoMembers[name] = toMojoSyncGroupMemberInfo(member)
	}
	return toMojoError(err), mojoMembers, nil
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

func (m *mojoImpl) TableGetPermissions(name string) (mojom.Error, mojom.Perms, error) {
	return mojom.Error{}, mojom.Perms{}, nil
}

func (m *mojoImpl) TableSetPermissions(name string, mPerms mojom.Perms) (mojom.Error, error) {
	return mojom.Error{}, nil
}

func (m *mojoImpl) TableDeleteRange(name string, start, limit []byte) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.TableDesc, "DeleteRange"))
	stub, err := m.getTable(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.DeleteRange(ctx, call, NoSchema, start, limit)
	return toMojoError(err), nil
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
	// proxy.OnKeyValue() blocks until the client acks the previous invocation,
	// thus providing flow control.
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

	go func() {
		var err = stub.Scan(ctx, tableScanServerCallStub, NoSchema, start, limit)

		// NOTE(nlacasse): Since we are already streaming, we send any error back
		// to the client on the stream.  The TableScan function itself should not
		// return an error at this point.
		proxy.OnReturn(toMojoError(err))
	}()

	return mojom.Error{}, nil
}

func (m *mojoImpl) TableGetPrefixPermissions(name, key string) (mojom.Error, []mojom.PrefixPerms, error) {
	return mojom.Error{}, nil, nil
}

func (m *mojoImpl) TableSetPrefixPermissions(name, prefix string, mPerms mojom.Perms) (mojom.Error, error) {
	return mojom.Error{}, nil
}

func (m *mojoImpl) TableDeletePrefixPermissions(name, prefix string) (mojom.Error, error) {
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
