// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build ignore
// +build cgo

// Syncbase C/Cgo API. Our strategy is to translate Cgo requests into Vanadium
// stub requests, and Vanadium stub responses into Cgo responses. As part of
// this procedure, we synthesize "fake" ctx and call objects to pass to the
// Vanadium stubs.

package bridge_cgo

import (
	"v.io/v23/context"
	"v.io/v23/glob"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	"v.io/v23/services/permissions"
	wire "v.io/v23/services/syncbase"
	watchwire "v.io/v23/services/watch"
	"v.io/v23/syncbase/util"
	"v.io/v23/vdl"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/v23/vtrace"
	"v.io/x/ref/services/syncbase/bridge"
)

// Global state, initialized by XInit.
var b *Bridge

func XInit() {
	// TODO(sadovsky): Support shutdown?
	ctx, _ = v23.Init()
	srv, disp, _ = syncbaselib.Serve(d.ctx, opts)
	b = bridge.NewBridge(ctx, srv, disp)
}

// FIXME: All the code below needs to be updated to present a cgo API instead of
// a mojo API.

////////////////////////////////////////
// Glob utils

func (m *mojoImpl) listChildren(name string) (mojom.Error, []string, error) {
	ctx, call := m.NewCtxCall(name, rpc.MethodDesc{
		Name: "GlobChildren__",
	})
	stub, err := m.GetGlobber(ctx, call, name)
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
		encName := v.Value[strings.LastIndex(v.Value, "/")+1:]
		// Component names within object names are always encoded. See comment in
		// server/dispatcher.go for explanation.
		name, ok := util.Decode(encName)
		if !ok {
			return verror.New(verror.ErrInternal, g.ctx, encName)
		}
		g.Results = append(g.Results, name)
	}

	return nil
}

////////////////////////////////////////
// Service

// TODO(sadovsky): All Mojo stub implementations return a nil error (the last
// return value), since that error doesn't make it back to the IPC client. Chat
// with rogulenko@ about whether we should change the Go Mojo stub generator to
// drop these errors.
func (m *mojoImpl) ServiceGetPermissions() (mojom.Error, mojom.Perms, string, error) {
	ctx, call := m.NewCtxCall("", bridge.MethodDesc(permissions.ObjectDesc, "GetPermissions"))
	stub, err := m.GetService(ctx, call)
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
	ctx, call := m.NewCtxCall("", bridge.MethodDesc(permissions.ObjectDesc, "SetPermissions"))
	stub, err := m.GetService(ctx, call)
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

func (m *mojoImpl) ServiceListDatabases() (mojom.Error, []mojom.Id, error) {
	return m.listChildren("")
}

////////////////////////////////////////
// Database

func (m *mojoImpl) DbCreate(name string, mPerms mojom.Perms) (mojom.Error, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.DatabaseDesc, "Create"))
	stub, err := m.GetDb(ctx, call, name)
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
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.DatabaseDesc, "Destroy"))
	stub, err := m.GetDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.Destroy(ctx, call)
	return toMojoError(err), nil
}

func (m *mojoImpl) DbExists(name string) (mojom.Error, bool, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.DatabaseDesc, "Exists"))
	stub, err := m.GetDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), false, nil
	}
	exists, err := stub.Exists(ctx, call)
	return toMojoError(err), exists, nil
}

func (m *mojoImpl) DbListCollections(name, batchHandle string) (mojom.Error, []string, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.DatabaseDesc, "ListCollections"))
	stub, err := m.GetDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil, nil
	}
	collections, err := stub.ListCollections(ctx, call)
	return toMojoError(err), collections, nil
}

type execStreamImpl struct {
	ctx   *context.T
	proxy *mojom.ExecStream_Proxy
}

func (s *execStreamImpl) Send(item interface{}) error {
	rb, ok := item.([]*vom.RawBytes)
	if !ok {
		return verror.NewErrInternal(s.ctx)
	}

	// TODO(aghassemi): Switch generic values on the wire from '[]byte' to 'any'.
	// Currently, exec is the only method that uses 'any' rather than '[]byte'.
	// https://github.com/vanadium/issues/issues/766
	var values [][]byte
	for _, raw := range rb {
		var bytes []byte
		// The value type can be either string (for column headers and row keys) or
		// []byte (for values).
		if raw.Type.Kind() == vdl.String {
			var str string
			if err := raw.ToValue(&str); err != nil {
				return err
			}
			bytes = []byte(str)
		} else {
			if err := raw.ToValue(&bytes); err != nil {
				return err
			}
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

func (m *mojoImpl) DbExec(name, batchHandle, query string, params [][]byte, ptr mojom.ExecStream_Pointer) (mojom.Error, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.DatabaseDesc, "Exec"))
	stub, err := m.GetDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}

	// TODO(ivanpi): For now, Dart always gives us []byte, and here we convert
	// []byte to vom.RawBytes as required by Exec. This will need to change once
	// we support VDL/VOM in Dart.
	// https://github.com/vanadium/issues/issues/766
	paramsVom := make([]*vom.RawBytes, len(params))
	for i, p := range params {
		var err error
		if paramsVom[i], err = vom.RawBytesFromValue(p); err != nil {
			return toMojoError(err), nil
		}
	}

	proxy := mojom.NewExecStreamProxy(ptr, bindings.GetAsyncWaiter())

	execServerCallStub := &wire.DatabaseExecServerCallStub{struct {
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
		var err = stub.Exec(ctx, execServerCallStub, query, paramsVom)
		// NOTE(nlacasse): Since we are already streaming, we send any error back
		// to the client on the stream.  The Exec function itself should not
		// return an error at this point.
		proxy.OnDone(toMojoError(err))
	}()

	return mojom.Error{}, nil
}

func (m *mojoImpl) DbBeginBatch(name string, bo *mojom.BatchOptions) (mojom.Error, string, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.DatabaseDesc, "BeginBatch"))
	stub, err := m.GetDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), "", nil
	}
	vbo := wire.BatchOptions{}
	if bo != nil {
		vbo.Hint = bo.Hint
		vbo.ReadOnly = bo.ReadOnly
	}
	batchSuffix, err := stub.BeginBatch(ctx, call, vbo)
	return toMojoError(err), batchSuffix, nil
}

func (m *mojoImpl) DbCommit(name, batchHandle string) (mojom.Error, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.DatabaseDesc, "Commit"))
	stub, err := m.GetDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.Commit(ctx, call)
	return toMojoError(err), nil
}

func (m *mojoImpl) DbAbort(name, batchHandle string) (mojom.Error, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.DatabaseDesc, "Abort"))
	stub, err := m.GetDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.Abort(ctx, call)
	return toMojoError(err), nil
}

func (m *mojoImpl) DbGetPermissions(name string) (mojom.Error, mojom.Perms, string, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(permissions.ObjectDesc, "GetPermissions"))
	stub, err := m.GetDb(ctx, call, name)
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
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(permissions.ObjectDesc, "SetPermissions"))
	stub, err := m.GetDb(ctx, call, name)
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
	vc := syncbase.ToWatchChange(c)

	var value []byte
	if vc.ChangeType != DeleteChange {
		if err := vc.Value(&value); err != nil {
			return err
		}
	}
	mc := mojom.WatchChange{
		CollectionName: vc.Collection,
		RowKey:         vc.Row,
		ChangeType:     uint32(vc.ChangeType),
		ValueBytes:     value,
		ResumeMarker:   vc.ResumeMarker,
		FromSync:       vc.FromSync,
		Continued:      vc.Continued,
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
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(watchwire.GlobWatcherDesc, "WatchGlob"))
	stub, err := m.GetDb(ctx, call, name)
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

func (m *mojoImpl) DbGetResumeMarker(name, batchHandle string) (mojom.Error, []byte, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.DatabaseWatcherDesc, "GetResumeMarker"))
	stub, err := m.GetDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil, nil
	}
	marker, err := stub.GetResumeMarker(ctx, call)
	return toMojoError(err), marker, nil
}

////////////////////////////////////////
// SyncgroupManager

func (m *mojoImpl) DbGetSyncgroupNames(name string) (mojom.Error, []string, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.SyncgroupManagerDesc, "GetSyncgroupNames"))
	stub, err := m.GetDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil, nil
	}
	names, err := stub.GetSyncgroupNames(ctx, call)
	return toMojoError(err), names, nil
}

func (m *mojoImpl) DbCreateSyncgroup(name, sgName string, spec mojom.SyncgroupSpec, myInfo mojom.SyncgroupMemberInfo) (mojom.Error, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.SyncgroupManagerDesc, "CreateSyncgroup"))
	stub, err := m.GetDb(ctx, call, name)
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
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.SyncgroupManagerDesc, "JoinSyncgroup"))
	stub, err := m.GetDb(ctx, call, name)
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
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.SyncgroupManagerDesc, "LeaveSyncgroup"))
	stub, err := m.GetDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	return toMojoError(stub.LeaveSyncgroup(ctx, call, sgName)), nil
}

func (m *mojoImpl) DbDestroySyncgroup(name, sgName string) (mojom.Error, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.SyncgroupManagerDesc, "DestroySyncgroup"))
	stub, err := m.GetDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	return toMojoError(stub.DestroySyncgroup(ctx, call, sgName)), nil
}

func (m *mojoImpl) DbEjectFromSyncgroup(name, sgName string, member string) (mojom.Error, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.SyncgroupManagerDesc, "EjectFromSyncgroup"))
	stub, err := m.GetDb(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	return toMojoError(stub.EjectFromSyncgroup(ctx, call, sgName, member)), nil
}

func (m *mojoImpl) DbGetSyncgroupSpec(name, sgName string) (mojom.Error, mojom.SyncgroupSpec, string, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.SyncgroupManagerDesc, "GetSyncgroupSpec"))
	stub, err := m.GetDb(ctx, call, name)
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
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.SyncgroupManagerDesc, "SetSyncgroupSpec"))
	stub, err := m.GetDb(ctx, call, name)
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
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.SyncgroupManagerDesc, "GetSyncgroupMembers"))
	stub, err := m.GetDb(ctx, call, name)
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
// Collection

func (m *mojoImpl) CollectionCreate(name, batchHandle string, mPerms mojom.Perms) (mojom.Error, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.CollectionDesc, "Create"))
	stub, err := m.GetCollection(ctx, call, name)
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

func (m *mojoImpl) CollectionDestroy(name, batchHandle string) (mojom.Error, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.CollectionDesc, "Destroy"))
	stub, err := m.GetCollection(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.Destroy(ctx, call)
	return toMojoError(err), nil
}

func (m *mojoImpl) CollectionExists(name, batchHandle string) (mojom.Error, bool, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.CollectionDesc, "Exists"))
	stub, err := m.GetCollection(ctx, call, name)
	if err != nil {
		return toMojoError(err), false, nil
	}
	exists, err := stub.Exists(ctx, call)
	return toMojoError(err), exists, nil
}

func (m *mojoImpl) CollectionGetPermissions(name, batchHandle string) (mojom.Error, mojom.Perms, error) {
	return toMojomError(verror.NewErrNotImplemented(nil)), mojom.Perms{}, nil
}

func (m *mojoImpl) CollectionSetPermissions(name, batchHandle string, mPerms mojom.Perms) (mojom.Error, error) {
	return toMojomError(verror.NewErrNotImplemented(nil)), nil
}

func (m *mojoImpl) CollectionDeleteRange(name, batchHandle string, start, limit []byte) (mojom.Error, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.CollectionDesc, "DeleteRange"))
	stub, err := m.GetCollection(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.DeleteRange(ctx, call, start, limit)
	return toMojoError(err), nil
}

type scanStreamImpl struct {
	ctx   *context.T
	proxy *mojom.ScanStream_Proxy
}

func (s *scanStreamImpl) Send(item interface{}) error {
	kv, ok := item.(wire.KeyValue)
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
func (m *mojoImpl) CollectionScan(name, batchHandle string, start, limit []byte, ptr mojom.ScanStream_Pointer) (mojom.Error, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.CollectionDesc, "Scan"))
	stub, err := m.GetCollection(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}

	proxy := mojom.NewScanStreamProxy(ptr, bindings.GetAsyncWaiter())

	collectionScanServerCallStub := &wire.CollectionScanServerCallStub{struct {
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
		var err = stub.Scan(ctx, collectionScanServerCallStub, start, limit)

		// NOTE(nlacasse): Since we are already streaming, we send any error back to
		// the client on the stream. The CollectionScan function itself should not
		// return an error at this point.
		proxy.OnDone(toMojoError(err))
	}()

	return mojom.Error{}, nil
}

////////////////////////////////////////
// Row

func (m *mojoImpl) RowExists(name, batchHandle string) (mojom.Error, bool, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.RowDesc, "Exists"))
	stub, err := m.GetRow(ctx, call, name)
	if err != nil {
		return toMojoError(err), false, nil
	}
	exists, err := stub.Exists(ctx, call)
	return toMojoError(err), exists, nil
}

func (m *mojoImpl) RowGet(name, batchHandle string) (mojom.Error, []byte, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.RowDesc, "Get"))
	stub, err := m.GetRow(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil, nil
	}
	vomBytes, err := stub.Get(ctx, call)

	var value []byte
	if err := vom.Decode(vomBytes, &value); err != nil {
		return toMojoError(err), nil, nil
	}
	return toMojoError(err), value, nil
}

func (m *mojoImpl) RowPut(name, batchHandle string, value []byte) (mojom.Error, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.RowDesc, "Put"))
	stub, err := m.GetRow(ctx, call, name)
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
	err = stub.Put(ctx, call, vomBytes)
	return toMojoError(err), nil
}

func (m *mojoImpl) RowDelete(name, batchHandle string) (mojom.Error, error) {
	ctx, call := m.NewCtxCall(name, bridge.MethodDesc(wire.RowDesc, "Delete"))
	stub, err := m.GetRow(ctx, call, name)
	if err != nil {
		return toMojoError(err), nil
	}
	err = stub.Delete(ctx, call)
	return toMojoError(err), nil
}
