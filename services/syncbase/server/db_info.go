// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

// This file defines internal app methods for manipulating dbInfo.
// None of these methods perform authorization checks.
//
// The fundamental reason why these methods are needed is that information about
// a database is spread across two storage engines. The source of truth for the
// existence of the database, as well as things like the database type, is the
// service-level storage engine, while database permissions are tracked in the
// database's storage engine.

import (
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
)

func dbInfoStKey(a *app, dbName string) string {
	return util.JoinKeyParts(util.DbInfoPrefix, a.stKeyPart(), dbName)
}

// getDbInfo reads data from the storage engine.
func (a *app) getDbInfo(ctx *context.T, st store.StoreReader, dbName string) (*dbInfo, error) {
	info := &dbInfo{}
	if err := util.Get(ctx, st, dbInfoStKey(a, dbName), info); err != nil {
		return nil, err
	}
	return info, nil
}

// putDbInfo writes data to the storage engine.
func (a *app) putDbInfo(ctx *context.T, st store.StoreWriter, dbName string, info *dbInfo) error {
	return util.Put(ctx, st, dbInfoStKey(a, dbName), info)
}

// delDbInfo deletes data from the storage engine.
func (a *app) delDbInfo(ctx *context.T, st store.StoreWriter, dbName string) error {
	return util.Delete(ctx, st, dbInfoStKey(a, dbName))
}

// updateDbInfo performs a read-modify-write. fn should "modify" v.
func (a *app) updateDbInfo(ctx *context.T, st store.StoreReadWriter, dbName string, fn func(info *dbInfo) error) error {
	_ = st.(store.Transaction) // panics on failure, as desired
	info, err := a.getDbInfo(ctx, st, dbName)
	if err != nil {
		return err
	}
	if err := fn(info); err != nil {
		return err
	}
	return a.putDbInfo(ctx, st, dbName, info)
}
