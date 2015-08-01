// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build mojo

package server

import (
	wire "mojom/syncbase"
)

type mojoImpl struct {
	s *service
}

func NewMojoImpl(s interface{}) *mojoImpl {
	return &mojoImpl{s: s.(*service)}
}

// TODO(sadovsky): Implement all stubs. The high-level plan is to translate Mojo
// requests into Vanadium requests by constructing suitable ctx and call objects
// and then performing the same work that server.Dispatcher and nosql.Dispatcher
// do (perhaps with some refactoring to share dispatcher code).

// TODO(sadovsky): Implement security hack, where we derive a client blessing
// from the Syncbase server's blessing.

////////////////////////////////////////
// Service

func (m *mojoImpl) ServiceGetPermissions() (wire.Error, wire.Perms, string, error) {
	return wire.Error{}, wire.Perms{}, "", nil
}

func (m *mojoImpl) ServiceSetPermissions(perms wire.Perms, version string) (wire.Error, error) {
	return wire.Error{}, nil
}

////////////////////////////////////////
// App

func (m *mojoImpl) AppCreate(name string, perms wire.Perms) (wire.Error, error) {
	return wire.Error{}, nil
}

func (m *mojoImpl) AppDelete(name string) (wire.Error, error) {
	return wire.Error{}, nil
}

func (m *mojoImpl) AppExists(name string) (wire.Error, bool, error) {
	return wire.Error{}, false, nil
}

func (m *mojoImpl) AppGetPermissions(name string) (wire.Error, wire.Perms, string, error) {
	return wire.Error{}, wire.Perms{}, "", nil
}

func (m *mojoImpl) AppSetPermissions(name string, perms wire.Perms, version string) (wire.Error, error) {
	return wire.Error{}, nil
}

////////////////////////////////////////
// nosql.Database

func (m *mojoImpl) DbCreate(name string, perms wire.Perms) (wire.Error, error) {
	return wire.Error{}, nil
}

func (m *mojoImpl) DbDelete(name string) (wire.Error, error) {
	return wire.Error{}, nil
}

func (m *mojoImpl) DbExists(name string) (wire.Error, bool, error) {
	return wire.Error{}, false, nil
}

func (m *mojoImpl) DbExec(name string, stream wire.ExecStream_Request) (wire.Error, error) {
	return wire.Error{}, nil
}

func (m *mojoImpl) DbBeginBatch(name string, bo *wire.BatchOptions) (wire.Error, string, error) {
	return wire.Error{}, "", nil
}

func (m *mojoImpl) DbCommit(name string) (wire.Error, error) {
	return wire.Error{}, nil
}

func (m *mojoImpl) DbAbort(name string) (wire.Error, error) {
	return wire.Error{}, nil
}

func (m *mojoImpl) DbGetPermissions(name string) (wire.Error, wire.Perms, string, error) {
	return wire.Error{}, wire.Perms{}, "", nil
}

func (m *mojoImpl) DbSetPermissions(name string, perms wire.Perms, version string) (wire.Error, error) {
	return wire.Error{}, nil
}

////////////////////////////////////////
// nosql.Database:SyncGroupManager

func (m *mojoImpl) DbGetSyncGroupNames(name string) (wire.Error, []string, error) {
	return wire.Error{}, nil, nil
}

func (m *mojoImpl) DbCreateSyncGroup(name, sgName string, spec wire.SyncGroupSpec, myInfo wire.SyncGroupMemberInfo) (wire.Error, error) {
	return wire.Error{}, nil
}

func (m *mojoImpl) DbJoinSyncGroup(name, sgName string, myInfo wire.SyncGroupMemberInfo) (wire.Error, error) {
	return wire.Error{}, nil
}

func (m *mojoImpl) DbLeaveSyncGroup(name, sgName string) (wire.Error, error) {
	return wire.Error{}, nil
}

func (m *mojoImpl) DbDestroySyncGroup(name, sgName string) (wire.Error, error) {
	return wire.Error{}, nil
}

func (m *mojoImpl) DbEjectFromSyncGroup(name, sgName string, member string) (wire.Error, error) {
	return wire.Error{}, nil
}

func (m *mojoImpl) DbGetSyncGroupSpec(name, sgName string) (wire.Error, wire.SyncGroupSpec, string, error) {
	return wire.Error{}, wire.SyncGroupSpec{}, "", nil
}

func (m *mojoImpl) DbSetSyncGroupSpec(name, sgName string, spec wire.SyncGroupSpec, version string) (wire.Error, error) {
	return wire.Error{}, nil
}

func (m *mojoImpl) DbGetSyncGroupMembers(name, sgName string) (wire.Error, map[string]wire.SyncGroupMemberInfo, error) {
	return wire.Error{}, nil, nil
}

////////////////////////////////////////
// nosql.Table

func (m *mojoImpl) TableCreate(name string, perms wire.Perms) (wire.Error, error) {
	return wire.Error{}, nil
}

func (m *mojoImpl) TableDelete(name string) (wire.Error, error) {
	return wire.Error{}, nil
}

func (m *mojoImpl) TableExists(name string) (wire.Error, bool, error) {
	return wire.Error{}, false, nil
}

func (m *mojoImpl) TableDeleteRowRange(name string, start, limit []byte) (wire.Error, error) {
	return wire.Error{}, nil
}

func (m *mojoImpl) TableScan(name string, start, limit []byte, stream wire.ScanStream_Request) (wire.Error, error) {
	return wire.Error{}, nil
}

func (m *mojoImpl) TableGetPermissions(name, key string) (wire.Error, []wire.PrefixPerms, error) {
	return wire.Error{}, nil, nil
}

func (m *mojoImpl) TableSetPermissions(name, prefix string, perms wire.Perms) (wire.Error, error) {
	return wire.Error{}, nil
}

func (m *mojoImpl) TableDeletePermissions(name, prefix string) (wire.Error, error) {
	return wire.Error{}, nil
}

////////////////////////////////////////
// nosql.Row

func (m *mojoImpl) RowExists(name string) (wire.Error, bool, error) {
	return wire.Error{}, false, nil
}

func (m *mojoImpl) RowGet(name string) (wire.Error, []byte, error) {
	return wire.Error{}, nil, nil
}

func (m *mojoImpl) RowPut(name string, value []byte) (wire.Error, error) {
	return wire.Error{}, nil
}

func (m *mojoImpl) RowDelete(name string) (wire.Error, error) {
	return wire.Error{}, nil
}
