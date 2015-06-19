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

type dbInfoLayer struct {
	name string
	a    *app
}

var (
	_ util.Layer = (*dbInfoLayer)(nil)
)

////////////////////////////////////////
// dbInfoLayer util.Layer methods

func (d *dbInfoLayer) Name() string {
	return d.name
}

func (d *dbInfoLayer) StKey() string {
	return util.JoinKeyParts(util.DbInfoPrefix, d.stKeyPart())
}

////////////////////////////////////////
// Internal helpers

func (d *dbInfoLayer) stKeyPart() string {
	return util.JoinKeyParts(d.a.stKeyPart(), d.name)
}

// getDbInfo reads data from the storage engine.
// Returns a VDL-compatible error.
func (a *app) getDbInfo(ctx *context.T, st store.StoreReader, dbName string) (*dbInfo, error) {
	info := &dbInfo{}
	if err := util.GetWithoutAuth(ctx, st, &dbInfoLayer{dbName, a}, info); err != nil {
		return nil, err
	}
	return info, nil
}

// putDbInfo writes data to the storage engine.
// Returns a VDL-compatible error.
func (a *app) putDbInfo(ctx *context.T, st store.StoreWriter, dbName string, info *dbInfo) error {
	return util.Put(ctx, st, &dbInfoLayer{dbName, a}, info)
}

// delDbInfo deletes data from the storage engine.
// Returns a VDL-compatible error.
func (a *app) delDbInfo(ctx *context.T, st store.StoreWriter, dbName string) error {
	return util.Delete(ctx, st, &dbInfoLayer{dbName, a})
}

// updateDbInfo performs a read-modify-write.
// fn should "modify" v, and should return a VDL-compatible error.
// Returns a VDL-compatible error.
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
