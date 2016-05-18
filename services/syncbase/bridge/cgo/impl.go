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
// - All exported function and type names start with "v23_syncbase_", to avoid
//   colliding with desired client library names.
// - Exported functions take input arguments by value, optional input arguments
//   by pointer, and output arguments by pointer.
// - Caller transfers ownership of all input arguments to callee; callee
//   transfers ownership of all output arguments to caller. If a function
//   returns an error, other output arguments need not be freed.
// - Variables with Cgo-specific types have names that start with "c".

package main

import (
	"encoding/base64"
	"os"
	"strings"
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/glob"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/services/permissions"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/syncbase/util"
	"v.io/v23/verror"
	"v.io/v23/vom"
	_ "v.io/x/ref/runtime/factories/roaming"
	"v.io/x/ref/services/syncbase/bridge"
	"v.io/x/ref/services/syncbase/syncbaselib"
)

/*
#include "lib.h"

static void CallCollectionScanCallbacksOnKeyValue(v23_syncbase_CollectionScanCallbacks cbs, v23_syncbase_KeyValue kv) {
  cbs.onKeyValue(cbs.hOnKeyValue, kv);
}
static void CallCollectionScanCallbacksOnDone(v23_syncbase_CollectionScanCallbacks cbs, v23_syncbase_VError err) {
  cbs.onDone(cbs.hOnKeyValue, cbs.hOnDone, err);
}
*/
import "C"

// Global state, initialized by v23_syncbase_Init.
var b *bridge.Bridge

//export v23_syncbase_Init
func v23_syncbase_Init() {
	// Strip all flags beyond the binary name; otherwise, v23.Init will fail when it encounters
	// unknown flags passed by Xcode, e.g. NSTreatUnknownArgumentsAsOpen.
	os.Args = os.Args[:1]
	// TODO(sadovsky): Support shutdown?
	ctx, _ := v23.Init()
	srv, disp, _ := syncbaselib.Serve(ctx, syncbaselib.Opts{})
	b = bridge.NewBridge(ctx, srv, disp)
}

////////////////////////////////////////
// Glob utils

func listChildIds(name string, cIds *C.v23_syncbase_Ids, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_ServiceGetPermissions
func v23_syncbase_ServiceGetPermissions(cPerms *C.v23_syncbase_Permissions, cVersion *C.v23_syncbase_String, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_ServiceSetPermissions
func v23_syncbase_ServiceSetPermissions(cPerms C.v23_syncbase_Permissions, cVersion C.v23_syncbase_String, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_ServiceListDatabases
func v23_syncbase_ServiceListDatabases(cIds *C.v23_syncbase_Ids, cErr *C.v23_syncbase_VError) {
	listChildIds("", cIds, cErr)
}

////////////////////////////////////////
// Database

//export v23_syncbase_DbCreate
func v23_syncbase_DbCreate(cName C.v23_syncbase_String, cPerms C.v23_syncbase_Permissions, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_DbDestroy
func v23_syncbase_DbDestroy(cName C.v23_syncbase_String, cErr *C.v23_syncbase_VError) {
	name := cName.toString()
	ctx, call := b.NewCtxCall(name, bridge.MethodDesc(wire.DatabaseDesc, "Destroy"))
	stub, err := b.GetDb(ctx, call, name)
	if err != nil {
		cErr.init(err)
		return
	}
	cErr.init(stub.Destroy(ctx, call))
}

//export v23_syncbase_DbExists
func v23_syncbase_DbExists(cName C.v23_syncbase_String, cExists *C.v23_syncbase_Bool, cErr *C.v23_syncbase_VError) {
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
	cExists.init(exists)
}

//export v23_syncbase_DbListCollections
func v23_syncbase_DbListCollections(cName, cBatchHandle C.v23_syncbase_String, cIds *C.v23_syncbase_Ids, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_DbBeginBatch
func v23_syncbase_DbBeginBatch(cName C.v23_syncbase_String, cOpts C.v23_syncbase_BatchOptions, cBatchHandle *C.v23_syncbase_String, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_DbCommit
func v23_syncbase_DbCommit(cName, cBatchHandle C.v23_syncbase_String, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_DbAbort
func v23_syncbase_DbAbort(cName, cBatchHandle C.v23_syncbase_String, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_DbGetPermissions
func v23_syncbase_DbGetPermissions(cName C.v23_syncbase_String, cPerms *C.v23_syncbase_Permissions, cVersion *C.v23_syncbase_String, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_DbSetPermissions
func v23_syncbase_DbSetPermissions(cName C.v23_syncbase_String, cPerms C.v23_syncbase_Permissions, cVersion C.v23_syncbase_String, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_DbGetResumeMarker
func v23_syncbase_DbGetResumeMarker(cName, cBatchHandle C.v23_syncbase_String, cMarker *C.v23_syncbase_Bytes, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_DbListSyncgroups
func v23_syncbase_DbListSyncgroups(cName C.v23_syncbase_String, cIds *C.v23_syncbase_Ids, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_DbCreateSyncgroup
func v23_syncbase_DbCreateSyncgroup(cName C.v23_syncbase_String, cSgId C.v23_syncbase_Id, cSpec C.v23_syncbase_SyncgroupSpec, cMyInfo C.v23_syncbase_SyncgroupMemberInfo, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_DbJoinSyncgroup
func v23_syncbase_DbJoinSyncgroup(cName C.v23_syncbase_String, cSgId C.v23_syncbase_Id, cMyInfo C.v23_syncbase_SyncgroupMemberInfo, cSpec *C.v23_syncbase_SyncgroupSpec, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_DbLeaveSyncgroup
func v23_syncbase_DbLeaveSyncgroup(cName C.v23_syncbase_String, cSgId C.v23_syncbase_Id, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_DbDestroySyncgroup
func v23_syncbase_DbDestroySyncgroup(cName C.v23_syncbase_String, cSgId C.v23_syncbase_Id, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_DbEjectFromSyncgroup
func v23_syncbase_DbEjectFromSyncgroup(cName C.v23_syncbase_String, cSgId C.v23_syncbase_Id, cMember C.v23_syncbase_String, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_DbGetSyncgroupSpec
func v23_syncbase_DbGetSyncgroupSpec(cName C.v23_syncbase_String, cSgId C.v23_syncbase_Id, cSpec *C.v23_syncbase_SyncgroupSpec, cVersion *C.v23_syncbase_String, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_DbSetSyncgroupSpec
func v23_syncbase_DbSetSyncgroupSpec(cName C.v23_syncbase_String, cSgId C.v23_syncbase_Id, cSpec C.v23_syncbase_SyncgroupSpec, cVersion C.v23_syncbase_String, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_DbGetSyncgroupMembers
func v23_syncbase_DbGetSyncgroupMembers(cName C.v23_syncbase_String, cSgId C.v23_syncbase_Id, cMembers *C.v23_syncbase_SyncgroupMemberInfoMap, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_CollectionCreate
func v23_syncbase_CollectionCreate(cName, cBatchHandle C.v23_syncbase_String, cPerms C.v23_syncbase_Permissions, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_CollectionDestroy
func v23_syncbase_CollectionDestroy(cName, cBatchHandle C.v23_syncbase_String, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_CollectionExists
func v23_syncbase_CollectionExists(cName, cBatchHandle C.v23_syncbase_String, cExists *C.v23_syncbase_Bool, cErr *C.v23_syncbase_VError) {
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
	cExists.init(exists)
}

//export v23_syncbase_CollectionGetPermissions
func v23_syncbase_CollectionGetPermissions(cName, cBatchHandle C.v23_syncbase_String, cPerms *C.v23_syncbase_Permissions, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_CollectionSetPermissions
func v23_syncbase_CollectionSetPermissions(cName, cBatchHandle C.v23_syncbase_String, cPerms C.v23_syncbase_Permissions, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_CollectionDeleteRange
func v23_syncbase_CollectionDeleteRange(cName, cBatchHandle C.v23_syncbase_String, cStart, cLimit C.v23_syncbase_Bytes, cErr *C.v23_syncbase_VError) {
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
	cbs C.v23_syncbase_CollectionScanCallbacks
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
	// C.CallCollectionScanCallbacksOnKeyValue() blocks until the client acks the
	// previous invocation, thus providing flow control.
	cKeyValue := C.v23_syncbase_KeyValue{}
	cKeyValue.init(kv.Key, value)
	C.CallCollectionScanCallbacksOnKeyValue(s.cbs, cKeyValue)
	return nil
}

func (s *scanStreamImpl) Recv(_ interface{}) error {
	// This should never be called.
	return verror.NewErrInternal(s.ctx)
}

var _ rpc.Stream = (*scanStreamImpl)(nil)

// TODO(nlacasse): Provide some way for the client to cancel the stream.

//export v23_syncbase_CollectionScan
func v23_syncbase_CollectionScan(cName, cBatchHandle C.v23_syncbase_String, cStart, cLimit C.v23_syncbase_Bytes, cbs C.v23_syncbase_CollectionScanCallbacks, cErr *C.v23_syncbase_VError) {
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
		cErr := C.v23_syncbase_VError{}
		cErr.init(err)
		C.CallCollectionScanCallbacksOnDone(cbs, cErr)
	}()
}

////////////////////////////////////////
// Row

//export v23_syncbase_RowExists
func v23_syncbase_RowExists(cName, cBatchHandle C.v23_syncbase_String, cExists *bool, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_RowGet
func v23_syncbase_RowGet(cName, cBatchHandle C.v23_syncbase_String, cValue *C.v23_syncbase_Bytes, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_RowPut
func v23_syncbase_RowPut(cName, cBatchHandle C.v23_syncbase_String, cValue C.v23_syncbase_Bytes, cErr *C.v23_syncbase_VError) {
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

//export v23_syncbase_RowDelete
func v23_syncbase_RowDelete(cName, cBatchHandle C.v23_syncbase_String, cErr *C.v23_syncbase_VError) {
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

////////////////////////////////////////
// Misc utilities

//export v23_syncbase_EncodeId
func v23_syncbase_EncodeId(cId C.v23_syncbase_Id, cEncoded *C.v23_syncbase_String) {
	cEncoded.init(util.EncodeId(cId.toId()))
}

//export v23_syncbase_Encode
func v23_syncbase_Encode(cName C.v23_syncbase_String, cEncoded *C.v23_syncbase_String) {
	cEncoded.init(util.Encode(cName.toString()))
}

//export v23_syncbase_Base64UrlDecode
func v23_syncbase_Base64UrlDecode(base64UrlEncoded C.v23_syncbase_String, cData *C.v23_syncbase_Bytes, cErr *C.v23_syncbase_VError) {
	// Decode the base64 url encoded string to bytes in a way that prevents extra copies along the Cgo boundary.
	urlEncoded := base64UrlEncoded.toString()
	b, err := base64.URLEncoding.DecodeString(urlEncoded)
	if err != nil {
		cErr.init(err)
		return
	}
	cData.init(b)
}

//export v23_syncbase_NamingJoin
func v23_syncbase_NamingJoin(cElements C.v23_syncbase_Strings, cJoined *C.v23_syncbase_String) {
	cJoined.init(naming.Join(cElements.toStrings()...))
}

////////////////////////////////////////
// Blessings
// TODO(zinman): This section will go away once we move to taking in an oauth token on init and hiding
// all blessings code in Go.

//export v23_syncbase_PublicKey
func v23_syncbase_PublicKey(cPublicKey *C.v23_syncbase_String, cErr *C.v23_syncbase_VError) {
	der, err := v23.GetPrincipal(b.Ctx).PublicKey().MarshalBinary()
	if err != nil {
		cErr.init(err)
		return
	}
	cPublicKey.init(base64.URLEncoding.EncodeToString(der))
}

//export v23_syncbase_SetVomEncodedBlessings
func v23_syncbase_SetVomEncodedBlessings(cBlessings C.v23_syncbase_Bytes, cErr *C.v23_syncbase_VError) {
	encodedBlessings := cBlessings.toBytes()
	var blessings security.Blessings
	if err := vom.Decode(encodedBlessings, &blessings); err != nil {
		cErr.init(err)
		return
	}
	principal := v23.GetPrincipal(b.Ctx)
	if err := principal.BlessingStore().SetDefault(blessings); err != nil {
		cErr.init(err)
		return
	}
}

//export v23_syncbase_BlessingsStoreDebugString
func v23_syncbase_BlessingsStoreDebugString(cDebugString *C.v23_syncbase_String) {
	cDebugString.init(v23.GetPrincipal(b.Ctx).BlessingStore().DebugString())
}

//export v23_syncbase_HasValidBlessings
func v23_syncbase_HasValidBlessings(cBool *C.v23_syncbase_Bool) {
	blessings, _ := v23.GetPrincipal(b.Ctx).BlessingStore().Default()
	// TODO(zinman): This is currently incorrect as it's almost always true that blessings aren't zero.
	// Need to figure out the security stuff better.
	cBool.init(!blessings.IsZero() && (blessings.Expiry().IsZero() || blessings.Expiry().After(time.Now())))
}

//export v23_syncbase_UserBlessingFromContext
func v23_syncbase_UserBlessingFromContext(cUserBlessing *C.v23_syncbase_String, cErr *C.v23_syncbase_VError) {
	b, err := util.UserBlessingFromContext(b.Ctx)
	if err != nil {
		cErr.init(err)
		return
	}
	cUserBlessing.init(b)
}

//export v23_syncbase_AppBlessingFromContext
func v23_syncbase_AppBlessingFromContext(cAppBlessing *C.v23_syncbase_String, cErr *C.v23_syncbase_VError) {
	b, err := util.AppBlessingFromContext(b.Ctx)
	if err != nil {
		cErr.init(err)
		return
	}
	cAppBlessing.init(b)
}
