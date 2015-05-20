// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nosql

import (
	wire "v.io/syncbase/v23/services/syncbase/nosql"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/verror"
)

////////////////////////////////////////
// SyncGroup RPC methods

func (d *database) GetSyncGroupNames(ctx *context.T, call rpc.ServerCall) ([]string, error) {
	return nil, verror.NewErrNotImplemented(ctx)
}

func (d *database) CreateSyncGroup(ctx *context.T, call rpc.ServerCall, sgName string, spec wire.SyncGroupSpec, myInfo wire.SyncGroupMemberInfo) error {
	return verror.NewErrNotImplemented(ctx)
}

func (d *database) JoinSyncGroup(ctx *context.T, call rpc.ServerCall, sgName string, myInfo wire.SyncGroupMemberInfo) (wire.SyncGroupSpec, error) {
	return wire.SyncGroupSpec{}, verror.NewErrNotImplemented(ctx)
}

func (d *database) LeaveSyncGroup(ctx *context.T, call rpc.ServerCall, sgName string) error {
	return verror.NewErrNotImplemented(ctx)
}

func (d *database) DestroySyncGroup(ctx *context.T, call rpc.ServerCall, sgName string) error {
	return verror.NewErrNotImplemented(ctx)
}

func (d *database) EjectFromSyncGroup(ctx *context.T, call rpc.ServerCall, sgName, member string) error {
	return verror.NewErrNotImplemented(ctx)
}

func (d *database) GetSyncGroupSpec(ctx *context.T, call rpc.ServerCall, sgName string) (wire.SyncGroupSpec, string, error) {
	return wire.SyncGroupSpec{}, "", verror.NewErrNotImplemented(ctx)
}

func (d *database) SetSyncGroupSpec(ctx *context.T, call rpc.ServerCall, sgName string, spec wire.SyncGroupSpec, version string) error {
	return verror.NewErrNotImplemented(ctx)
}

func (d *database) GetSyncGroupMembers(ctx *context.T, call rpc.ServerCall, sgName string) (map[string]wire.SyncGroupMemberInfo, error) {
	return nil, verror.NewErrNotImplemented(ctx)
}
