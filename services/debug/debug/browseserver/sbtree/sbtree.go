// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package sbtree provides data structures used by HTML templates to build the
// web pages for the Syncbase debug viewer.  To minimize mixing of code and
// presentation, all the data for use in the templates is in a form that is
// convenient for accessing and iterating over, i.e struct fields, no-arg
// methods, slices, or maps.  In some cases this required mirroring data
// structures in the public Syncbase API to avoid having the templates deal with
// context objects, or to avoid the templates needing extra variables to handle
// indirection.
package sbtree

import (
	"errors"
	"fmt"

	"v.io/v23/context"
	"v.io/v23/rpc/reserved"
	wire "v.io/v23/services/syncbase"
	"v.io/v23/syncbase"
)

// SyncbaseTree has all the data for the main page of the Syncbase debug viewer.
type SyncbaseTree struct {
	Service syncbase.Service
	Dbs     []dbTree
}

type dbTree struct {
	Database    syncbase.Database
	Collections []syncbase.Collection
	Syncgroups  []syncgroupTree
}

type syncgroupTree struct {
	Syncgroup syncbase.Syncgroup
	Spec      wire.SyncgroupSpec
	Members   map[string]wire.SyncgroupMemberInfo
}

// NoSyncbaseError returned as error from AssembleSbTree when the given server
// does not implement the Syncbase RPC interface.
var NoSyncbaseError = errors.New("Server does not have Syncbase")

// AssembleSbTree returns information describing the Syncbase server running on
// the given server. One possible error it can return is NoSyncbaseError,
// indicating that the server does not implement the Syncbase RPC interface.
func AssembleSyncbaseTree(ctx *context.T, server string) (*SyncbaseTree, error) {
	hasSyncbase, err := hasSyncbaseService(ctx, server)
	if err != nil {
		return nil, fmt.Errorf("Problem getting interfaces: %v", err)
	}
	if !hasSyncbase {
		return nil, NoSyncbaseError
	}

	service := syncbase.NewService(server)

	dbIds, err := service.ListDatabases(ctx)
	if err != nil {
		return nil, fmt.Errorf("Problem listing databases: %v", err)
	}

	dbTrees := make([]dbTree, len(dbIds))
	for i := range dbIds {
		// TODO(eobrain) Confirm nil for schema is appropriate
		db := service.DatabaseForId(dbIds[i], nil)

		// Assemble collections
		collIds, err := db.ListCollections(ctx)
		if err != nil {
			return nil, fmt.Errorf("Problem listing collections: %v", err)
		}
		colls := make([]syncbase.Collection, len(collIds))
		for j := range collIds {
			colls[j] = db.CollectionForId(collIds[j])
		}

		// Assemble syncgroups
		sgIds, err := db.ListSyncgroups(ctx)
		if err != nil {
			return nil, fmt.Errorf("Problem listing syncgroups: %v", err)
		}
		sgs := make([]syncgroupTree, len(sgIds))
		for j := range sgIds {
			sg := db.SyncgroupForId(sgIds[j])
			spec, _, err := sg.GetSpec(ctx)
			if err != nil {
				return nil, fmt.Errorf("Problem getting spec of syncgroup: %v", err)
			}
			members, err := sg.GetMembers(ctx)
			if err != nil {
				return nil, fmt.Errorf("Problem getting members of syncgroup: %v", err)
			}
			sgs[j] = syncgroupTree{sg, spec, members}
		}

		dbTrees[i] = dbTree{db, colls, sgs}
	}

	return &SyncbaseTree{service, dbTrees}, nil
}

// hasSyncbaseService determines whether the given server implements the
// Syncbase interface.
func hasSyncbaseService(ctx *context.T, server string) (bool, error) {
	const (
		syncbasePkgPath = "v.io/v23/services/syncbase"
		syncbaseName    = "Service"
	)
	interfaces, err := reserved.Signature(ctx, server)
	if err != nil {
		return false, err
	}
	for _, ifc := range interfaces {
		if ifc.Name == syncbaseName && ifc.PkgPath == syncbasePkgPath {
			return true, nil
		}
	}
	return false, nil
}
