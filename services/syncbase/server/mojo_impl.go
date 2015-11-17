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
	"v.io/v23/glob"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	"v.io/v23/services/permissions"
	wire "v.io/v23/services/syncbase"
	nosqlwire "v.io/v23/services/syncbase/nosql"
	watchwire "v.io/v23/services/watch"
	"v.io/v23/syncbase/nosql"
	"v.io/v23/syncbase/util"
	"v.io/v23/vdl"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/v23/vtrace"
)

const noSchema int32 = -1

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

func toV23SyncgroupMemberInfo(mInfo mojom.SyncgroupMemberInfo) nosqlwire.SyncgroupMemberInfo {
	return nosqlwire.SyncgroupMemberInfo{
		SyncPriority: mInfo.SyncPriority,
	}
}

func toMojoSyncgroupMemberInfo(vInfo nosqlwire.SyncgroupMemberInfo) mojom.SyncgroupMemberInfo {
	return mojom.SyncgroupMemberInfo{
		SyncPriority: vInfo.SyncPriority,
	}
}

func toV23SyncgroupSpec(mSpec mojom.SyncgroupSpec) (nosqlwire.SyncgroupSpec, error) {
	v23Perms, err := toV23Perms(mSpec.Perms)
	if err != nil {
		return nosqlwire.SyncgroupSpec{}, err
	}
	prefixes := make([]nosqlwire.TableRow, len(mSpec.Prefixes))
	for i, v := range mSpec.Prefixes {
		prefixes[i].TableName = v.TableName
		prefixes[i].Row = v.Row
	}
	return nosqlwire.SyncgroupSpec{
		Description: mSpec.Description,
		Perms:       v23Perms,
		Prefixes:    prefixes,
		MountTables: mSpec.MountTables,
		IsPrivate:   mSpec.IsPrivate,
	}, nil
}

func toMojoSyncgroupSpec(vSpec nosqlwire.SyncgroupSpec) (mojom.SyncgroupSpec, error) {
	mPerms, err := toMojoPerms(vSpec.Perms)
	if err != nil {
		return mojom.SyncgroupSpec{}, err
	}
	prefixes := make([]mojom.TableRow, len(vSpec.Prefixes))
	for i, v := range vSpec.Prefixes {
		prefixes[i].TableName = v.TableName
		prefixes[i].Row = v.Row
	}
	return mojom.SyncgroupSpec{
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

func (m *mojoImpl) getGlobber(ctx *context.T, call rpc.ServerCall, name string) (rpc.ChildrenGlobber, error) {
	resInt, err := m.lookupAndAuthorize(ctx, call, name)
	if err != nil {
		return nil, err
	}
	if res, ok := resInt.(rpc.Globber); !ok {
		return nil, verror.NewErrInternal(ctx)
	} else if res.Globber() == nil || res.Globber().ChildrenGlobber == nil {
		return nil, verror.NewErrInternal(ctx)
	} else {
		return res.Globber().ChildrenGlobber, nil
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
// Glob utils

func (m *mojoImpl) listChildren(name string) (mojom.Error, []string, error) {
	ctx, call := m.newCtxCall(name, rpc.MethodDesc{
		Name: "GlobChildren__",
	})
	stub, err := m.getGlobber(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil, nil
	}
	gcsCall := &globChildrenServerCall{call, ctx, make([]string, 0)}
	g, err := glob.Parse("*")
	if err != nil {
		return toMojoError(err), nil, nil
	}
	err = stub.GlobChildren__(ctx, gcsCall, g.Head())
	return toMojoError(err), gcsCall.Results, nil
}

type globChildrenServerCall struct {
	rpc.ServerCall
	ctx     *context.T
	Results []string
}

func (g *globChildrenServerCall) SendStream() interface {
	Send(naming.GlobChildrenReply) error
} {
	return g
}

func (g *globChildrenServerCall) Send(reply naming.GlobChildrenReply) error {
	if v, ok := reply.(naming.GlobChildrenReplyName); ok {
		escName := v.Value[strings.LastIndex(v.Value, "/")+1:]
		// Component names within object names are always escaped. See comment in
		// server/nosql/dispatcher.go for explanation.
		name, ok := util.Unescape(escName)
		if !ok {
			return verror.New(verror.ErrInternal, g.ctx, escName)
		}
		g.Results = append(g.Results, name)
	}

	return nil
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

func (m *mojoImpl) ServiceListApps() (mojom.Error, []string, error) {
	return m.listChildren("")
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

func (m *mojoImpl) AppListDatabases(name string) (mojom.Error, []string, error) {
	return m.listChildren(name)
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
	err = stub.Destroy(ctx, call, noSchema)
	return toMojoError(err), nil
}

func (m *mojoImpl) DbExists(name string) (mojom.Error, bool, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.DatabaseDesc, "Exists"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), false, nil
	}
	exists, err := stub.Exists(ctx, call, noSchema)
	return toMojoError(err), exists, nil
}

type execStreamImpl struct {
	ctx   *context.T
	proxy *mojom.ExecStream_Proxy
}

func (s *execStreamImpl) Send(item interface{}) error {
	v, ok := item.([]*vdl.Value)
	if !ok {
		return verror.NewErrInternal(s.ctx)
	}

	// TODO(aghassemi): Switch generic values on the wire from '[]byte' to 'any'.
	// Currently, exec is the only method that uses 'any' rather than '[]byte'.
	// https://github.com/vanadium/issues/issues/766
	var values [][]byte
	for _, vdlValue := range v {
		var bytes []byte
		// The value type can be either string (for column headers and row keys) or
		// []byte (for values).
		if vdlValue.Kind() == vdl.String {
			bytes = []byte(vdlValue.RawString())
		} else {
			bytes = vdlValue.Bytes()
		}
		values = append(values, bytes)
	}

	r := mojom.Result{
		Values: values,
	}

	// proxy.OnResult() blocks until the client acks the previous invocation,
	// thus providing flow control.
	return s.proxy.OnResult(r)
}

func (s *execStreamImpl) Recv(_ interface{}) error {
	// This should never be called.
	return verror.NewErrInternal(s.ctx)
}

var _ rpc.Stream = (*execStreamImpl)(nil)

func (m *mojoImpl) DbExec(name string, query string, ptr mojom.ExecStream_Pointer) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.DatabaseDesc, "Exec"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}

	proxy := mojom.NewExecStreamProxy(ptr, bindings.GetAsyncWaiter())

	execServerCallStub := &nosqlwire.DatabaseExecServerCallStub{struct {
		rpc.Stream
		rpc.ServerCall
	}{
		&execStreamImpl{
			ctx:   ctx,
			proxy: proxy,
		},
		call,
	}}

	go func() {
		var err = stub.Exec(ctx, execServerCallStub, noSchema, query)
		// NOTE(nlacasse): Since we are already streaming, we send any error back
		// to the client on the stream.  The Exec function itself should not
		// return an error at this point.
		proxy.OnDone(toMojoError(err))
	}()

	return mojom.Error{}, nil
}

func (m *mojoImpl) DbBeginBatch(name string, bo *mojom.BatchOptions) (mojom.Error, string, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.DatabaseDesc, "BeginBatch"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), "", nil
	}
	vbo := nosqlwire.BatchOptions{}
	if bo != nil {
		vbo.Hint = bo.Hint
		vbo.ReadOnly = bo.ReadOnly
	}
	batchSuffix, err := stub.BeginBatch(ctx, call, noSchema, vbo)
	return toMojoError(err), batchSuffix, nil
}

func (m *mojoImpl) DbCommit(name string) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.DatabaseDesc, "Commit"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.Commit(ctx, call, noSchema)
	return toMojoError(err), nil
}

func (m *mojoImpl) DbAbort(name string) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.DatabaseDesc, "Abort"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.Abort(ctx, call, noSchema)
	return toMojoError(err), nil
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

	var value []byte
	if vc.ValueBytes != nil {
		if err := vom.Decode(vc.ValueBytes, &value); err != nil {
			return err
		}
	}
	mc := mojom.WatchChange{
		TableName:    vc.Table,
		RowKey:       vc.Row,
		ChangeType:   uint32(vc.ChangeType),
		ValueBytes:   value,
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
		// NOTE(aghassemi): WatchGlob does not terminate unless there is an error.
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

func (m *mojoImpl) DbListTables(name string) (mojom.Error, []string, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.DatabaseDesc, "ListTables"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil, nil
	}
	tables, err := stub.ListTables(ctx, call)
	return toMojoError(err), tables, nil
}

////////////////////////////////////////
// nosql.Database:SyncgroupManager

func (m *mojoImpl) DbGetSyncgroupNames(name string) (mojom.Error, []string, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.SyncgroupManagerDesc, "GetSyncgroupNames"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil, nil
	}
	names, err := stub.GetSyncgroupNames(ctx, call)
	return toMojoError(err), names, nil
}

func (m *mojoImpl) DbCreateSyncgroup(name, sgName string, spec mojom.SyncgroupSpec, myInfo mojom.SyncgroupMemberInfo) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.SyncgroupManagerDesc, "CreateSyncgroup"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	v23Spec, err := toV23SyncgroupSpec(spec)
	if err != nil {
		return toMojoError(err), nil
	}
	return toMojoError(stub.CreateSyncgroup(ctx, call, sgName, v23Spec, toV23SyncgroupMemberInfo(myInfo))), nil
}

func (m *mojoImpl) DbJoinSyncgroup(name, sgName string, myInfo mojom.SyncgroupMemberInfo) (mojom.Error, mojom.SyncgroupSpec, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.SyncgroupManagerDesc, "JoinSyncgroup"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), mojom.SyncgroupSpec{}, nil
	}
	spec, err := stub.JoinSyncgroup(ctx, call, sgName, toV23SyncgroupMemberInfo(myInfo))
	if err != nil {
		return toMojoError(err), mojom.SyncgroupSpec{}, nil
	}
	mojoSpec, err := toMojoSyncgroupSpec(spec)
	if err != nil {
		return toMojoError(err), mojom.SyncgroupSpec{}, nil
	}
	return toMojoError(err), mojoSpec, nil
}

func (m *mojoImpl) DbLeaveSyncgroup(name, sgName string) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.SyncgroupManagerDesc, "LeaveSyncgroup"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	return toMojoError(stub.LeaveSyncgroup(ctx, call, sgName)), nil
}

func (m *mojoImpl) DbDestroySyncgroup(name, sgName string) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.SyncgroupManagerDesc, "DestroySyncgroup"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	return toMojoError(stub.DestroySyncgroup(ctx, call, sgName)), nil
}

func (m *mojoImpl) DbEjectFromSyncgroup(name, sgName string, member string) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.SyncgroupManagerDesc, "EjectFromSyncgroup"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	return toMojoError(stub.EjectFromSyncgroup(ctx, call, sgName, member)), nil
}

func (m *mojoImpl) DbGetSyncgroupSpec(name, sgName string) (mojom.Error, mojom.SyncgroupSpec, string, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.SyncgroupManagerDesc, "GetSyncgroupSpec"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), mojom.SyncgroupSpec{}, "", nil
	}
	spec, version, err := stub.GetSyncgroupSpec(ctx, call, sgName)
	mojoSpec, err := toMojoSyncgroupSpec(spec)
	if err != nil {
		return toMojoError(err), mojom.SyncgroupSpec{}, "", nil
	}
	return toMojoError(err), mojoSpec, version, nil
}

func (m *mojoImpl) DbSetSyncgroupSpec(name, sgName string, spec mojom.SyncgroupSpec, version string) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.SyncgroupManagerDesc, "SetSyncgroupSpec"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	v23Spec, err := toV23SyncgroupSpec(spec)
	if err != nil {
		return toMojoError(err), nil
	}
	return toMojoError(stub.SetSyncgroupSpec(ctx, call, sgName, v23Spec, version)), nil
}

func (m *mojoImpl) DbGetSyncgroupMembers(name, sgName string) (mojom.Error, map[string]mojom.SyncgroupMemberInfo, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.SyncgroupManagerDesc, "GetSyncgroupMembers"))
	stub, err := m.getDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil, nil
	}
	members, err := stub.GetSyncgroupMembers(ctx, call, sgName)
	if err != nil {
		return toMojoError(err), nil, nil
	}
	mojoMembers := make(map[string]mojom.SyncgroupMemberInfo, len(members))
	for name, member := range members {
		mojoMembers[name] = toMojoSyncgroupMemberInfo(member)
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
	err = stub.Create(ctx, call, noSchema, vPerms)
	return toMojoError(err), nil
}

func (m *mojoImpl) TableDestroy(name string) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.TableDesc, "Destroy"))
	stub, err := m.getTable(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.Destroy(ctx, call, noSchema)
	return toMojoError(err), nil
}

func (m *mojoImpl) TableExists(name string) (mojom.Error, bool, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.TableDesc, "Exists"))
	stub, err := m.getTable(ctx, call, name)
	if err != nil {
		return toMojoError(err), false, nil
	}
	exists, err := stub.Exists(ctx, call, noSchema)
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
	err = stub.DeleteRange(ctx, call, noSchema, start, limit)
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

	var value []byte
	if err := vom.Decode(kv.Value, &value); err != nil {
		return err
	}
	// proxy.OnKeyValue() blocks until the client acks the previous invocation,
	// thus providing flow control.
	return s.proxy.OnKeyValue(mojom.KeyValue{
		Key:   kv.Key,
		Value: value,
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
		var err = stub.Scan(ctx, tableScanServerCallStub, noSchema, start, limit)

		// NOTE(nlacasse): Since we are already streaming, we send any error back
		// to the client on the stream.  The TableScan function itself should not
		// return an error at this point.
		proxy.OnDone(toMojoError(err))
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
	exists, err := stub.Exists(ctx, call, noSchema)
	return toMojoError(err), exists, nil
}

func (m *mojoImpl) RowGet(name string) (mojom.Error, []byte, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.RowDesc, "Get"))
	stub, err := m.getRow(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil, nil
	}
	vomBytes, err := stub.Get(ctx, call, noSchema)

	var value []byte
	if err := vom.Decode(vomBytes, &value); err != nil {
		return toMojoError(err), nil, nil
	}
	return toMojoError(err), value, nil
}

func (m *mojoImpl) RowPut(name string, value []byte) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.RowDesc, "Put"))
	stub, err := m.getRow(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	// TODO(aghassemi): For now, Dart always gives us []byte, and here we convert
	// []byte to VOM-encoded []byte so that all values stored in Syncbase are
	// VOM-encoded. This will need to change once we support VDL/VOM in Dart.
	// https://github.com/vanadium/issues/issues/766
	vomBytes, err := vom.Encode(value)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.Put(ctx, call, noSchema, vomBytes)
	return toMojoError(err), nil
}

func (m *mojoImpl) RowDelete(name string) (mojom.Error, error) {
	ctx, call := m.newCtxCall(name, methodDesc(nosqlwire.RowDesc, "Delete"))
	stub, err := m.getRow(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.Delete(ctx, call, noSchema)
	return toMojoError(err), nil
}
