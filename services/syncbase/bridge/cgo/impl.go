// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build cgo

// Syncbase C/Cgo API. Our strategy is to translate Cgo requests into Vanadium
// stub requests, and Vanadium stub responses into Cgo responses. As part of
// this procedure, we synthesize "fake" ctx and call objects to pass to the
// Vanadium stubs.
//
// Implementation notes:
// - All exported function and type names start with "X", to avoid colliding
//   with desired client library names.
// - Exported functions take input arguments by value, optional input arguments
//   by pointer, and output arguments by pointer.
// - Caller transfers ownership of all input arguments to callee; callee
//   transfers ownership of all output arguments to caller. If a function
//   returns an error, other output arguments need not be freed.
// - Variables with Cgo-specific types have names that start with "c".

// TODO(sadovsky): Prefix exported names with something better than "X", e.g.
// with "v23_syncbase_".

package main

import (
	"strings"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/glob"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/services/permissions"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/syncbase/util"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/ref/services/syncbase/bridge"
	"v.io/x/ref/services/syncbase/syncbaselib"
)

/*
#include "lib.h"

static void CallXCollectionScanCallbacksOnKeyValue(XCollectionScanCallbacks cbs, XKeyValue kv) {
  cbs.onKeyValue(cbs.hOnKeyValue, kv);
}
static void CallXCollectionScanCallbacksOnDone(XCollectionScanCallbacks cbs, XVError err) {
  cbs.onDone(cbs.hOnKeyValue, cbs.hOnDone, err);
}
*/
import "C"

// Global state, initialized by XInit.
var b *bridge.Bridge

//export XInit
func XInit() {
	// TODO(sadovsky): Support shutdown?
	ctx, _ := v23.Init()
	srv, disp, _ := syncbaselib.Serve(ctx, syncbaselib.Opts{})
	b = bridge.NewBridge(ctx, srv, disp)
}

////////////////////////////////////////
// Glob utils

func listChildIds(name string, cIds *C.XIds, cErr *C.XVError) {
	ctx, call := b.NewCtxCall(name, rpc.MethodDesc{
		Name: "GlobChildren__",
	})
	stub, err := b.GetGlobber(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	gcsCall := &globChildrenServerCall{call, ctx, make([]wire.Id, 0)}
	g, err := glob.Parse("*")
	if err != nil {
		cErr.init(err)
		return
	}
	if err := stub.GlobChildren__(ctx, gcsCall, g.Head()); err != nil {
		cErr.init(err)
		return
	}
	cIds.init(gcsCall.Ids)
}

type globChildrenServerCall struct {
	rpc.ServerCall
	ctx *context.T
	Ids []wire.Id
}

func (g *globChildrenServerCall) SendStream() interface {
	Send(naming.GlobChildrenReply) error
} {
	return g
}

func (g *globChildrenServerCall) Send(reply naming.GlobChildrenReply) error {
	switch v := reply.(type) {
	case *naming.GlobChildrenReplyName:
		encId := v.Value[strings.LastIndex(v.Value, "/")+1:]
		// Component ids within object names are always encoded. See comment in
		// server/dispatcher.go for explanation.
		id, err := util.DecodeId(encId)
		if err != nil {
			// If this happens, there's a bug in the Syncbase server. Glob should
			// return names with escaped components.
			return verror.New(verror.ErrInternal, nil, err)
		}
		g.Ids = append(g.Ids, id)
	case *naming.GlobChildrenReplyError:
		return verror.New(verror.ErrInternal, nil, v.Value.Error)
	}
	return nil
}

////////////////////////////////////////
// Service

//export XServiceGetPermissions
func XServiceGetPermissions(cPerms *C.XPermissions, cVersion *C.XString, cErr *C.XVError) {
	ctx, call := b.NewCtxCall("", bridge.MethodDesc(permissions.ObjectDesc, "GetPermissions"))
	stub, err := b.GetService(ctx, call)
	if err != nil {
		cErr.init(err)
		return
	}
	perms, version, err := stub.GetPermissions(ctx, call)
	if err != nil {
		cErr.init(err)
		return
	}
	cPerms.init(perms)
	cVersion.init(version)
}

//export XServiceSetPermissions
func XServiceSetPermissions(cPerms C.XPermissions, cVersion C.XString, cErr *C.XVError) {
	perms := cPerms.toPermissions()
	version := cVersion.toString()
	ctx, call := b.NewCtxCall("", bridge.MethodDesc(permissions.ObjectDesc, "SetPermissions"))
	stub, err := b.GetService(ctx, call)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(stub.SetPermissions(ctx, call, perms, version))
}

//export XServiceListDatabases
func XServiceListDatabases(cIds *C.XIds, cErr *C.XVError) {
	listChildIds("", cIds, cErr)
}

////////////////////////////////////////
// Database

//export XDbCreate
func XDbCreate(cName C.XString, cPerms C.XPermissions, cErr *C.XVError) {
	name := cName.toString()
	perms := cPerms.toPermissions()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.DatabaseDesc, "Create"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(stub.Create(ctx, call, nil, perms))
}

//export XDbDestroy
func XDbDestroy(cName C.XString, cErr *C.XVError) {
	name := cName.toString()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.DatabaseDesc, "Destroy"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(stub.Destroy(ctx, call))
}

//export XDbExists
func XDbExists(cName C.XString, cExists *bool, cErr *C.XVError) {
	name := cName.toString()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.DatabaseDesc, "Exists"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	exists, err := stub.Exists(ctx, call)
	if err != nil {
		cErr.init(err)
		return
	}
	*cExists = exists
}

//export XDbListCollections
func XDbListCollections(cName, cBatchHandle C.XString, cIds *C.XIds, cErr *C.XVError) {
	name := cName.toString()
	batchHandle := wire.BatchHandle(cBatchHandle.toString())
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.DatabaseDesc, "ListCollections"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	ids, err := stub.ListCollections(ctx, call, batchHandle)
	if err != nil {
		cErr.init(err)
		return
	}
	cIds.init(ids)
}

//export XDbBeginBatch
func XDbBeginBatch(cName C.XString, cOpts C.XBatchOptions, cBatchHandle *C.XString, cErr *C.XVError) {
	name := cName.toString()
	opts := cOpts.toBatchOptions()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.DatabaseDesc, "BeginBatch"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	batchHandle, err := stub.BeginBatch(ctx, call, opts)
	if err != nil {
		cErr.init(err)
		return
	}
	cBatchHandle.init(string(batchHandle))
}

//export XDbCommit
func XDbCommit(cName, cBatchHandle C.XString, cErr *C.XVError) {
	name := cName.toString()
	batchHandle := wire.BatchHandle(cBatchHandle.toString())
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.DatabaseDesc, "Commit"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(stub.Commit(ctx, call, batchHandle))
}

//export XDbAbort
func XDbAbort(cName, cBatchHandle C.XString, cErr *C.XVError) {
	name := cName.toString()
	batchHandle := wire.BatchHandle(cBatchHandle.toString())
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.DatabaseDesc, "Abort"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(stub.Abort(ctx, call, batchHandle))
}

//export XDbGetPermissions
func XDbGetPermissions(cName C.XString, cPerms *C.XPermissions, cVersion *C.XString, cErr *C.XVError) {
	name := cName.toString()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(permissions.ObjectDesc, "GetPermissions"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	perms, version, err := stub.GetPermissions(ctx, call)
	if err != nil {
		cErr.init(err)
		return
	}
	cPerms.init(perms)
	cVersion.init(version)
}

//export XDbSetPermissions
func XDbSetPermissions(cName C.XString, cPerms C.XPermissions, cVersion C.XString, cErr *C.XVError) {
	name := cName.toString()
	perms := cPerms.toPermissions()
	version := cVersion.toString()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(permissions.ObjectDesc, "SetPermissions"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(stub.SetPermissions(ctx, call, perms, version))
}

// TODO(sadovsky): Add watch API.

//export XDbGetResumeMarker
func XDbGetResumeMarker(cName, cBatchHandle C.XString, cMarker *C.XBytes, cErr *C.XVError) {
	name := cName.toString()
	batchHandle := wire.BatchHandle(cBatchHandle.toString())
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.DatabaseWatcherDesc, "GetResumeMarker"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	marker, err := stub.GetResumeMarker(ctx, call, batchHandle)
	if err != nil {
		cErr.init(err)
		return
	}
	cMarker.init(marker)
}

////////////////////////////////////////
// SyncgroupManager

// FIXME(sadovsky): Implement "NewErrNotImplemented" methods below.

//export XDbListSyncgroups
func XDbListSyncgroups(cName C.XString, cIds *C.XIds, cErr *C.XVError) {
	name := cName.toString()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.SyncgroupManagerDesc, "ListSyncgroups"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(verror.NewErrNotImplemented(nil))
	_ = stub // prevent "declared and not used"
}

//export XDbCreateSyncgroup
func XDbCreateSyncgroup(cName C.XString, cSgId C.XId, cSpec C.XSyncgroupSpec, cMyInfo C.XSyncgroupMemberInfo, cErr *C.XVError) {
	name := cName.toString()
	sgId := cSgId.toId()
	spec := cSpec.toSyncgroupSpec()
	myInfo := cMyInfo.toSyncgroupMemberInfo()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.SyncgroupManagerDesc, "CreateSyncgroup"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(verror.NewErrNotImplemented(nil))
	_, _, _, _ = sgId, spec, myInfo, stub // prevent "declared and not used"
}

//export XDbJoinSyncgroup
func XDbJoinSyncgroup(cName C.XString, cSgId C.XId, cMyInfo C.XSyncgroupMemberInfo, cSpec *C.XSyncgroupSpec, cErr *C.XVError) {
	name := cName.toString()
	sgId := cSgId.toId()
	myInfo := cMyInfo.toSyncgroupMemberInfo()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.SyncgroupManagerDesc, "JoinSyncgroup"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(verror.NewErrNotImplemented(nil))
	_, _, _ = sgId, myInfo, stub // prevent "declared and not used"
}

//export XDbLeaveSyncgroup
func XDbLeaveSyncgroup(cName C.XString, cSgId C.XId, cErr *C.XVError) {
	name := cName.toString()
	sgId := cSgId.toId()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.SyncgroupManagerDesc, "LeaveSyncgroup"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(verror.NewErrNotImplemented(nil))
	_, _ = sgId, stub // prevent "declared and not used"
}

//export XDbDestroySyncgroup
func XDbDestroySyncgroup(cName C.XString, cSgId C.XId, cErr *C.XVError) {
	name := cName.toString()
	sgId := cSgId.toId()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.SyncgroupManagerDesc, "DestroySyncgroup"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(verror.NewErrNotImplemented(nil))
	_, _ = sgId, stub // prevent "declared and not used"
}

//export XDbEjectFromSyncgroup
func XDbEjectFromSyncgroup(cName C.XString, cSgId C.XId, cMember C.XString, cErr *C.XVError) {
	name := cName.toString()
	sgId := cSgId.toId()
	member := cMember.toString()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.SyncgroupManagerDesc, "EjectFromSyncgroup"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(verror.NewErrNotImplemented(nil))
	_, _, _ = sgId, member, stub // prevent "declared and not used"
}

//export XDbGetSyncgroupSpec
func XDbGetSyncgroupSpec(cName C.XString, cSgId C.XId, cSpec *C.XSyncgroupSpec, cVersion *C.XString, cErr *C.XVError) {
	name := cName.toString()
	sgId := cSgId.toId()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.SyncgroupManagerDesc, "GetSyncgroupSpec"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(verror.NewErrNotImplemented(nil))
	_, _ = sgId, stub // prevent "declared and not used"
}

//export XDbSetSyncgroupSpec
func XDbSetSyncgroupSpec(cName C.XString, cSgId C.XId, cSpec C.XSyncgroupSpec, cVersion C.XString, cErr *C.XVError) {
	name := cName.toString()
	sgId := cSgId.toId()
	spec := cSpec.toSyncgroupSpec()
	version := cVersion.toString()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.SyncgroupManagerDesc, "SetSyncgroupSpec"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(verror.NewErrNotImplemented(nil))
	_, _, _, _ = sgId, spec, version, stub // prevent "declared and not used"
}

//export XDbGetSyncgroupMembers
func XDbGetSyncgroupMembers(cName C.XString, cSgId C.XId, cMembers *C.XSyncgroupMemberInfoMap, cErr *C.XVError) {
	name := cName.toString()
	sgId := cSgId.toId()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.SyncgroupManagerDesc, "GetSyncgroupMembers"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(verror.NewErrNotImplemented(nil))
	_, _ = sgId, stub // prevent "declared and not used"
}

////////////////////////////////////////
// Collection

//export XCollectionCreate
func XCollectionCreate(cName, cBatchHandle C.XString, cPerms C.XPermissions, cErr *C.XVError) {
	name := cName.toString()
	batchHandle := wire.BatchHandle(cBatchHandle.toString())
	perms := cPerms.toPermissions()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.CollectionDesc, "Create"))
	stub, err := b.GetCollection(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(stub.Create(ctx, call, batchHandle, perms))
}

//export XCollectionDestroy
func XCollectionDestroy(cName, cBatchHandle C.XString, cErr *C.XVError) {
	name := cName.toString()
	batchHandle := wire.BatchHandle(cBatchHandle.toString())
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.CollectionDesc, "Destroy"))
	stub, err := b.GetCollection(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(stub.Destroy(ctx, call, batchHandle))
}

//export XCollectionExists
func XCollectionExists(cName, cBatchHandle C.XString, cExists *bool, cErr *C.XVError) {
	name := cName.toString()
	batchHandle := wire.BatchHandle(cBatchHandle.toString())
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.CollectionDesc, "Exists"))
	stub, err := b.GetCollection(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	exists, err := stub.Exists(ctx, call, batchHandle)
	if err != nil {
		cErr.init(err)
		return
	}
	*cExists = exists
}

//export XCollectionGetPermissions
func XCollectionGetPermissions(cName, cBatchHandle C.XString, cPerms *C.XPermissions, cErr *C.XVError) {
	name := cName.toString()
	batchHandle := wire.BatchHandle(cBatchHandle.toString())
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.CollectionDesc, "GetPermissions"))
	stub, err := b.GetCollection(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	perms, err := stub.GetPermissions(ctx, call, batchHandle)
	if err != nil {
		cErr.init(err)
		return
	}
	cPerms.init(perms)
}

//export XCollectionSetPermissions
func XCollectionSetPermissions(cName, cBatchHandle C.XString, cPerms C.XPermissions, cErr *C.XVError) {
	name := cName.toString()
	batchHandle := wire.BatchHandle(cBatchHandle.toString())
	perms := cPerms.toPermissions()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.CollectionDesc, "SetPermissions"))
	stub, err := b.GetCollection(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(stub.SetPermissions(ctx, call, batchHandle, perms))
}

//export XCollectionDeleteRange
func XCollectionDeleteRange(cName, cBatchHandle C.XString, cStart, cLimit C.XBytes, cErr *C.XVError) {
	name := cName.toString()
	batchHandle := wire.BatchHandle(cBatchHandle.toString())
	start, limit := cStart.toBytes(), cLimit.toBytes()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.CollectionDesc, "DeleteRange"))
	stub, err := b.GetCollection(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(stub.DeleteRange(ctx, call, batchHandle, start, limit))
}

type scanStreamImpl struct {
	ctx *context.T
	cbs C.XCollectionScanCallbacks
}

func (s *scanStreamImpl) Send(item interface{}) error {
	kv, ok := item.(wire.KeyValue)
	if !ok {
		return verror.NewErrInternal(s.ctx)
	}
	value, err := vom.Encode(kv.Value)
	if err != nil {
		return err
	}
	// C.CallXCollectionScanCallbacksOnKeyValue() blocks until the client acks the
	// previous invocation, thus providing flow control.
	cKeyValue := C.XKeyValue{}
	cKeyValue.init(kv.Key, value)
	C.CallXCollectionScanCallbacksOnKeyValue(s.cbs, cKeyValue)
	return nil
}

func (s *scanStreamImpl) Recv(_ interface{}) error {
	// This should never be called.
	return verror.NewErrInternal(s.ctx)
}

var _ rpc.Stream = (*scanStreamImpl)(nil)

// TODO(nlacasse): Provide some way for the client to cancel the stream.

//export XCollectionScan
func XCollectionScan(cName, cBatchHandle C.XString, cStart, cLimit C.XBytes, cbs C.XCollectionScanCallbacks, cErr *C.XVError) {
	name := cName.toString()
	batchHandle := wire.BatchHandle(cBatchHandle.toString())
	start, limit := cStart.toBytes(), cLimit.toBytes()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.CollectionDesc, "Scan"))
	stub, err := b.GetCollection(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}

	collectionScanServerCallStub := &wire.CollectionScanServerCallStub{struct {
		rpc.Stream
		rpc.ServerCall
	}{
		&scanStreamImpl{ctx: ctx, cbs: cbs},
		call,
	}}

	go func() {
		err := stub.Scan(ctx, collectionScanServerCallStub, batchHandle, start, limit)
		// NOTE(nlacasse): Since we are already streaming, we send any error back to
		// the client on the stream. The CollectionScan function itself should not
		// return an error at this point.
		cErr := C.XVError{}
		cErr.init(err)
		C.CallXCollectionScanCallbacksOnDone(cbs, cErr)
	}()
}

////////////////////////////////////////
// Row

//export XRowExists
func XRowExists(cName, cBatchHandle C.XString, cExists *bool, cErr *C.XVError) {
	name := cName.toString()
	batchHandle := wire.BatchHandle(cBatchHandle.toString())
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.RowDesc, "Exists"))
	stub, err := b.GetRow(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	exists, err := stub.Exists(ctx, call, batchHandle)
	if err != nil {
		cErr.init(err)
		return
	}
	*cExists = exists
}

//export XRowGet
func XRowGet(cName, cBatchHandle C.XString, cValue *C.XBytes, cErr *C.XVError) {
	name := cName.toString()
	batchHandle := wire.BatchHandle(cBatchHandle.toString())
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.RowDesc, "Get"))
	stub, err := b.GetRow(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	valueAsRawBytes, err := stub.Get(ctx, call, batchHandle)
	value, err := vom.Encode(valueAsRawBytes)
	if err != nil {
		cErr.init(err)
		return
	}
	cValue.init(value)
}

//export XRowPut
func XRowPut(cName, cBatchHandle C.XString, cValue C.XBytes, cErr *C.XVError) {
	name := cName.toString()
	batchHandle := wire.BatchHandle(cBatchHandle.toString())
	value := cValue.toBytes()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.RowDesc, "Put"))
	stub, err := b.GetRow(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	var valueAsRawBytes vom.RawBytes
	if err := vom.Decode(value, &valueAsRawBytes); err != nil {
		cErr.init(err)
		return
	}
	cErr.init(stub.Put(ctx, call, batchHandle, &valueAsRawBytes))
}

//export XRowDelete
func XRowDelete(cName, cBatchHandle C.XString, cErr *C.XVError) {
	name := cName.toString()
	batchHandle := wire.BatchHandle(cBatchHandle.toString())
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.RowDesc, "Delete"))
	stub, err := b.GetRow(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(stub.Delete(ctx, call, batchHandle))
}
