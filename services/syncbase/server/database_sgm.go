// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import (
	"v.io/v23/context"
	"v.io/v23/rpc"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/verror"
	"v.io/x/ref/services/syncbase/vsync"
)

////////////////////////////////////////
// Syncgroup RPC methods

func (d *databaseReq) GetSyncgroupNames(ctx *context.T, call rpc.ServerCall) ([]string, error) {
	if !d.exists {
		return nil, verror.New(verror.ErrNoExist, ctx, d.id)
	}
	if d.batchId != nil {
		return nil, wire.NewErrBoundToBatch(ctx)
	}
	sd := vsync.NewSyncDatabase(d)
	return sd.GetSyncgroupNames(ctx, call)
}

func (d *databaseReq) CreateSyncgroup(ctx *context.T, call rpc.ServerCall, sgName string, spec wire.SyncgroupSpec, myInfo wire.SyncgroupMemberInfo) error {
	if !d.exists {
		return verror.New(verror.ErrNoExist, ctx, d.id)
	}
	if d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	sd := vsync.NewSyncDatabase(d)
	return sd.CreateSyncgroup(ctx, call, sgName, spec, myInfo)
}

func (d *databaseReq) JoinSyncgroup(ctx *context.T, call rpc.ServerCall, sgName string, myInfo wire.SyncgroupMemberInfo) (wire.SyncgroupSpec, error) {
	if !d.exists {
		return wire.SyncgroupSpec{}, verror.New(verror.ErrNoExist, ctx, d.id)
	}
	if d.batchId != nil {
		return wire.SyncgroupSpec{}, wire.NewErrBoundToBatch(ctx)
	}
	sd := vsync.NewSyncDatabase(d)
	return sd.JoinSyncgroup(ctx, call, sgName, myInfo)
}

func (d *databaseReq) LeaveSyncgroup(ctx *context.T, call rpc.ServerCall, sgName string) error {
	if !d.exists {
		return verror.New(verror.ErrNoExist, ctx, d.id)
	}
	if d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	return verror.NewErrNotImplemented(ctx)
}

func (d *databaseReq) DestroySyncgroup(ctx *context.T, call rpc.ServerCall, sgName string) error {
	if !d.exists {
		return verror.New(verror.ErrNoExist, ctx, d.id)
	}
	if d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	return verror.NewErrNotImplemented(ctx)
}

func (d *databaseReq) EjectFromSyncgroup(ctx *context.T, call rpc.ServerCall, sgName, member string) error {
	if !d.exists {
		return verror.New(verror.ErrNoExist, ctx, d.id)
	}
	if d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	return verror.NewErrNotImplemented(ctx)
}

func (d *databaseReq) GetSyncgroupSpec(ctx *context.T, call rpc.ServerCall, sgName string) (wire.SyncgroupSpec, string, error) {
	if !d.exists {
		return wire.SyncgroupSpec{}, "", verror.New(verror.ErrNoExist, ctx, d.id)
	}
	if d.batchId != nil {
		return wire.SyncgroupSpec{}, "", wire.NewErrBoundToBatch(ctx)
	}
	sd := vsync.NewSyncDatabase(d)
	return sd.GetSyncgroupSpec(ctx, call, sgName)
}

func (d *databaseReq) SetSyncgroupSpec(ctx *context.T, call rpc.ServerCall, sgName string, spec wire.SyncgroupSpec, version string) error {
	if !d.exists {
		return verror.New(verror.ErrNoExist, ctx, d.id)
	}
	if d.batchId != nil {
		return wire.NewErrBoundToBatch(ctx)
	}
	sd := vsync.NewSyncDatabase(d)
	return sd.SetSyncgroupSpec(ctx, call, sgName, spec, version)
}

func (d *databaseReq) GetSyncgroupMembers(ctx *context.T, call rpc.ServerCall, sgName string) (map[string]wire.SyncgroupMemberInfo, error) {
	if !d.exists {
		return nil, verror.New(verror.ErrNoExist, ctx, d.id)
	}
	if d.batchId != nil {
		return nil, wire.NewErrBoundToBatch(ctx)
	}
	sd := vsync.NewSyncDatabase(d)
	return sd.GetSyncgroupMembers(ctx, call, sgName)
}
