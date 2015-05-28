// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// SyncGroup management and storage in Syncbase.  Handles the lifecycle
// of SyncGroups (create, join, leave, etc.) and their persistence as
// sync metadata in the application databases.  Provides helper functions
// to the higher levels of sync (Initiator, Watcher) to get membership
// information and map key/value changes to their matching SyncGroups.

import (
	"fmt"
	"time"

	"v.io/syncbase/v23/services/syncbase/nosql"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/verror"
	"v.io/v23/vom"
)

var (
	// memberViewTTL is the shelf-life of the aggregate view of SyncGroup members.
	memberViewTTL = 2 * time.Second
)

////////////////////////////////////////////////////////////
// SyncGroup management internal to Syncbase.

// memberView holds an aggregated view of all SyncGroup members across databases.
// The view is not coherent, it gets refreshed according to a configured TTL and
// not (coherently) when SyncGroup membership is updated in the various databases.
// It is needed by the sync Initiator, which must select a peer to contact from a
// global view of all SyncGroup members gathered from all databases.  This is why
// a slightly stale view is acceptable.
// The members are identified by their Vanadium names (map keys).
type memberView struct {
	expiration time.Time
	members    map[string]*memberInfo
}

// memberInfo holds the member metadata for each SyncGroup this member belongs to.
type memberInfo struct {
	gid2info map[GroupId]nosql.SyncGroupMemberInfo
}

// newSyncGroupVersion generates a random SyncGroup version ("etag").
func newSyncGroupVersion() string {
	return fmt.Sprintf("%x", rng.Int63())
}

// addSyncGroup adds a new SyncGroup given its information.
func addSyncGroup(ctx *context.T, tx store.StoreReadWriter, sg *SyncGroup) error {
	_ = tx.(store.Transaction)

	if sg == nil {
		return verror.New(verror.ErrBadArg, ctx, "group information not specified")
	}
	if sg.Name == "" {
		return verror.New(verror.ErrBadArg, ctx, "group name not specified")
	}
	if sg.Id == NoGroupId {
		return verror.New(verror.ErrBadArg, ctx, "group ID not specified")
	}
	if sg.Version == "" {
		return verror.New(verror.ErrBadArg, ctx, "group version not specified")
	}
	if len(sg.Joiners) == 0 {
		return verror.New(verror.ErrBadArg, ctx, "group has no joiners")
	}
	if len(sg.Spec.Prefixes) == 0 {
		return verror.New(verror.ErrBadArg, ctx, "group has no prefixes specified")
	}

	if hasSGDataEntry(tx, sg.Id) {
		return verror.New(verror.ErrBadArg, ctx, "group id already exists")
	}
	if hasSGNameEntry(tx, sg.Name) {
		return verror.New(verror.ErrBadArg, ctx, "group name already exists")
	}

	// Add the group name and data entries.
	if err := setSGNameEntry(ctx, tx, sg.Name, sg.Id); err != nil {
		return err
	}
	if err := setSGDataEntry(ctx, tx, sg.Id, sg); err != nil {
		return err
	}

	return nil
}

// getSyncGroupId retrieves the SyncGroup ID given its name.
func getSyncGroupId(ctx *context.T, st store.StoreReader, name string) (GroupId, error) {
	return getSGNameEntry(ctx, st, name)
}

// getSyncGroupName retrieves the SyncGroup name given its ID.
func getSyncGroupName(ctx *context.T, st store.StoreReader, gid GroupId) (string, error) {
	sg, err := getSyncGroupById(ctx, st, gid)
	if err != nil {
		return "", err
	}
	return sg.Name, nil
}

// getSyncGroupById retrieves the SyncGroup given its ID.
func getSyncGroupById(ctx *context.T, st store.StoreReader, gid GroupId) (*SyncGroup, error) {
	return getSGDataEntry(ctx, st, gid)
}

// getSyncGroupByName retrieves the SyncGroup given its name.
func getSyncGroupByName(ctx *context.T, st store.StoreReader, name string) (*SyncGroup, error) {
	gid, err := getSyncGroupId(ctx, st, name)
	if err != nil {
		return nil, err
	}
	return getSyncGroupById(ctx, st, gid)
}

// delSyncGroupById deletes the SyncGroup given its ID.
func delSyncGroupById(ctx *context.T, tx store.StoreReadWriter, gid GroupId) error {
	_ = tx.(store.Transaction)

	sg, err := getSyncGroupById(ctx, tx, gid)
	if err != nil {
		return err
	}
	if err = delSGNameEntry(ctx, tx, sg.Name); err != nil {
		return err
	}
	return delSGDataEntry(ctx, tx, sg.Id)
}

// delSyncGroupByName deletes the SyncGroup given its name.
func delSyncGroupByName(ctx *context.T, tx store.StoreReadWriter, name string) error {
	_ = tx.(store.Transaction)

	gid, err := getSyncGroupId(ctx, tx, name)
	if err != nil {
		return err
	}
	return delSyncGroupById(ctx, tx, gid)
}

// refreshMembersIfExpired updates the aggregate view of SyncGroup members across
// databases if the view has expired.
// TODO(rdaoud): track dirty apps/dbs since the last refresh and incrementally
// update the membership view for them instead of always scanning all of them.
func (s *syncService) refreshMembersIfExpired(ctx *context.T) {
	view := s.allMembers
	if view == nil {
		// The empty expiration time in Go is before "now" and treated as expired below.
		view = &memberView{expiration: time.Time{}, members: make(map[string]*memberInfo)}
		s.allMembers = view
	}

	if time.Now().Before(view.expiration) {
		return
	}

	// Create a new aggregate view of SyncGroup members across all app databases.
	// Get the apps and iterate over them.
	appNames, err := s.sv.AppNames(ctx, nil)
	if err != nil {
		return
	}

	newMembers := make(map[string]*memberInfo)
	scanStart, scanLimit := util.ScanPrefixArgs(sgDataKeyScanPrefix(), "")

	// For each app, get its databases and iterate over them.
	for _, a := range appNames {
		app, err := s.sv.App(ctx, nil, a)
		if err != nil {
			continue
		}
		dbNames, err := app.NoSQLDatabaseNames(ctx, nil)
		if err != nil {
			continue
		}

		// For each database, fetch its SyncGroup data entries by scanning their
		// prefix range.  Use a database snapshot for the scan.
		for _, d := range dbNames {
			db, err := app.NoSQLDatabase(ctx, nil, d)
			if err != nil {
				continue
			}

			sn := db.St().NewSnapshot()
			stream := sn.Scan(scanStart, scanLimit)
			for stream.Advance() {
				var sg SyncGroup
				if vom.Decode(stream.Value(nil), &sg) != nil {
					continue
				}

				// Add all members of this SyncGroup to the membership view.
				for member, info := range sg.Joiners {
					if _, ok := newMembers[member]; !ok {
						newMembers[member] = &memberInfo{
							gid2info: make(map[GroupId]nosql.SyncGroupMemberInfo),
						}
					}
					newMembers[member].gid2info[sg.Id] = info
				}
			}

			sn.Close()
		}
	}

	view.members = newMembers
	view.expiration = time.Now().Add(memberViewTTL)
}

// getMembers returns all SyncGroup members and the count of SyncGroups each one joined.
func (s *syncService) getMembers(ctx *context.T) map[string]uint32 {
	s.refreshMembersIfExpired(ctx)

	members := make(map[string]uint32)
	for member, info := range s.allMembers.members {
		members[member] = uint32(len(info.gid2info))
	}

	return members
}

// Low-level utility functions to access DB entries without tracking their relationships.
// Use the functions above to manipulate SyncGroups.

// sgDataKeyScanPrefix returns the prefix used to scan SyncGroup data entries.
func sgDataKeyScanPrefix() string {
	return util.JoinKeyParts(util.SyncPrefix, "sg", "d")
}

// sgDataKey returns the key used to access the SyncGroup data entry.
func sgDataKey(gid GroupId) string {
	return util.JoinKeyParts(util.SyncPrefix, "sg", "d", fmt.Sprintf("%d", gid))
}

// sgNameKey returns the key used to access the SyncGroup name entry.
func sgNameKey(name string) string {
	return util.JoinKeyParts(util.SyncPrefix, "sg", "n", name)
}

// hasSGDataEntry returns true if the SyncGroup data entry exists.
func hasSGDataEntry(st store.StoreReader, gid GroupId) bool {
	// TODO(rdaoud): optimize to avoid the unneeded fetch/decode of the data.
	var sg SyncGroup
	if err := util.GetObject(st, sgDataKey(gid), &sg); err != nil {
		return false
	}
	return true
}

// hasSGNameEntry returns true if the SyncGroup name entry exists.
func hasSGNameEntry(st store.StoreReader, name string) bool {
	// TODO(rdaoud): optimize to avoid the unneeded fetch/decode of the data.
	var gid GroupId
	if err := util.GetObject(st, sgNameKey(name), &gid); err != nil {
		return false
	}
	return true
}

// setSGDataEntry stores the SyncGroup data entry.
func setSGDataEntry(ctx *context.T, tx store.StoreReadWriter, gid GroupId, sg *SyncGroup) error {
	_ = tx.(store.Transaction)

	if err := util.PutObject(tx, sgDataKey(gid), sg); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// setSGNameEntry stores the SyncGroup name entry.
func setSGNameEntry(ctx *context.T, tx store.StoreReadWriter, name string, gid GroupId) error {
	_ = tx.(store.Transaction)

	if err := util.PutObject(tx, sgNameKey(name), gid); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// getSGDataEntry retrieves the SyncGroup data for a given group ID.
func getSGDataEntry(ctx *context.T, st store.StoreReader, gid GroupId) (*SyncGroup, error) {
	var sg SyncGroup
	if err := util.GetObject(st, sgDataKey(gid), &sg); err != nil {
		return nil, verror.New(verror.ErrInternal, ctx, err)
	}
	return &sg, nil
}

// getSGNameEntry retrieves the SyncGroup name to ID mapping.
func getSGNameEntry(ctx *context.T, st store.StoreReader, name string) (GroupId, error) {
	var gid GroupId
	if err := util.GetObject(st, sgNameKey(name), &gid); err != nil {
		return gid, verror.New(verror.ErrInternal, ctx, err)
	}
	return gid, nil
}

// delSGDataEntry deletes the SyncGroup data entry.
func delSGDataEntry(ctx *context.T, tx store.StoreReadWriter, gid GroupId) error {
	_ = tx.(store.Transaction)

	if err := tx.Delete([]byte(sgDataKey(gid))); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// delSGNameEntry deletes the SyncGroup name to ID mapping.
func delSGNameEntry(ctx *context.T, tx store.StoreReadWriter, name string) error {
	_ = tx.(store.Transaction)

	if err := tx.Delete([]byte(sgNameKey(name))); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

////////////////////////////////////////////////////////////
// SyncGroup methods between Client and Syncbase.

////////////////////////////////////////////////////////////
// Methods for SyncGroup create/join between Syncbases.

func (s *syncService) CreateSyncGroup(ctx *context.T, call rpc.ServerCall) error {
	return verror.NewErrNotImplemented(ctx)
}

func (s *syncService) JoinSyncGroup(ctx *context.T, call rpc.ServerCall) error {
	return verror.NewErrNotImplemented(ctx)
}
