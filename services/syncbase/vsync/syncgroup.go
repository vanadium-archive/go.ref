// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// SyncGroup management and storage in Syncbase.  Handle the lifecycle
// of SyncGroup (create, join, leave, etc.) and their persistence as
// sync metadata in the application databases.  Provide helper functions
// to the higher levels of sync (Initiator, Watcher) to get membership
// information and map key/value changes to their matching SyncGroups.

import (
	"errors" // TODO(rdaoud): switch to verror
	"fmt"
	"math/rand"
	"time"

	"v.io/syncbase/v23/services/syncbase/nosql"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/verror"
)

var (
	// memberViewTTL is the shelf-life of the aggregate view of SyncGroup members.
	memberViewTTL = 2 * time.Second

	// sgRng is a random number generator used for SyncGroup versions.
	sgRng *rand.Rand
)

func init() {
	sgRng = rand.New(rand.NewSource(time.Now().UTC().UnixNano()))
}

////////////////////////////////////////////////////////////
// SyncGroup management internal to Syncbase.

// memberView holds an aggregated view of all SyncGroup members across databases.
// The view is not coherent, it gets refreshed according to a configured TTL and
// not (coherently) when SyncGroup membership is updated in the various databases.
// It is needed by the sync Initiator when selecting a peer to contact from a
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
	return fmt.Sprintf("%x", sgRng.Int63())
}

// addSyncGroup adds a new SyncGroup given its information.
func addSyncGroup(tx store.StoreReadWriter, sg *SyncGroup) error {
	if sg == nil {
		return errors.New("group information not specified")
	}
	if sg.Name == "" {
		return errors.New("group name not specified")
	}
	if sg.Id == NoGroupId {
		return errors.New("group ID not specified")
	}
	if sg.Version == "" {
		return errors.New("group version not specified")
	}
	if len(sg.Joiners) == 0 {
		return errors.New("group has no joiners")
	}
	if len(sg.Spec.Prefixes) == 0 {
		return errors.New("group has no prefixes specified")
	}

	if hasSGDataEntry(tx, sg.Id) {
		return fmt.Errorf("group %d already exists", sg.Id)
	}
	if hasSGNameEntry(tx, sg.Name) {
		return fmt.Errorf("group name %s already exists", sg.Name)
	}

	// Add the group name and data entries.
	if err := setSGNameEntry(tx, sg.Name, sg.Id); err != nil {
		return err
	}
	if err := setSGDataEntry(tx, sg.Id, sg); err != nil {
		return err
	}

	return nil
}

// getSyncGroupId retrieves the SyncGroup ID given its name.
func getSyncGroupId(st store.StoreReader, name string) (GroupId, error) {
	return getSGNameEntry(st, name)
}

// getSyncGroupName retrieves the SyncGroup name given its ID.
func getSyncGroupName(st store.StoreReader, gid GroupId) (string, error) {
	sg, err := getSyncGroupById(st, gid)
	if err != nil {
		return "", err
	}
	return sg.Name, nil
}

// getSyncGroupById retrieves the SyncGroup given its ID.
func getSyncGroupById(st store.StoreReader, gid GroupId) (*SyncGroup, error) {
	return getSGDataEntry(st, gid)
}

// getSyncGroupByName retrieves the SyncGroup given its name.
func getSyncGroupByName(st store.StoreReader, name string) (*SyncGroup, error) {
	gid, err := getSyncGroupId(st, name)
	if err != nil {
		return nil, err
	}
	return getSyncGroupById(st, gid)
}

// delSyncGroupById deletes the SyncGroup given its ID.
func delSyncGroupById(tx store.StoreReadWriter, gid GroupId) error {
	sg, err := getSyncGroupById(tx, gid)
	if err != nil {
		return err
	}
	if err = delSGNameEntry(tx, sg.Name); err != nil {
		return err
	}
	return delSGDataEntry(tx, sg.Id)
}

// delSyncGroupByName deletes the SyncGroup given its name.
func delSyncGroupByName(tx store.StoreReadWriter, name string) error {
	gid, err := getSyncGroupId(tx, name)
	if err != nil {
		return err
	}
	return delSyncGroupById(tx, gid)
}

// refreshMembersIfExpired updates the aggregate view of SyncGroup members across
// databases if the view has expired.
func (s *syncService) refreshMembersIfExpired() {
	view := s.allMembers
	if view == nil {
		// The empty expiration time in Go is before "now" and treated as expired below.
		view = &memberView{expiration: time.Time{}, members: make(map[string]*memberInfo)}
		s.allMembers = view
	}

	if time.Now().Before(view.expiration) {
		return
	}

	// TODO(rdaoud): iterate over all SyncGroups in all app DBs to get members.
	// TODO(rdaoud): pending a new Syncbase API to access apps and database handles.

	view.expiration = time.Now().Add(memberViewTTL)
}

// getMembers returns all SyncGroup members and the count of SyncGroups each one joined.
func (s *syncService) getMembers() map[string]uint32 {
	s.refreshMembersIfExpired()

	members := make(map[string]uint32)
	for member, info := range s.allMembers.members {
		members[member] = uint32(len(info.gid2info))
	}

	return members
}

// Low-level utility functions to access DB entries without tracking their relationships.
// Use the functions above to manipulate SyncGroups.

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
func setSGDataEntry(tx store.StoreReadWriter, gid GroupId, sg *SyncGroup) error {
	return util.PutObject(tx, sgDataKey(gid), sg)
}

// setSGNameEntry stores the SyncGroup name entry.
func setSGNameEntry(tx store.StoreReadWriter, name string, gid GroupId) error {
	return util.PutObject(tx, sgNameKey(name), gid)
}

// getSGDataEntry retrieves the SyncGroup data for a given group ID.
func getSGDataEntry(st store.StoreReader, gid GroupId) (*SyncGroup, error) {
	var sg SyncGroup
	if err := util.GetObject(st, sgDataKey(gid), &sg); err != nil {
		return nil, err
	}
	return &sg, nil
}

// getSGNameEntry retrieves the SyncGroup name to ID mapping.
func getSGNameEntry(st store.StoreReader, name string) (GroupId, error) {
	var gid GroupId
	err := util.GetObject(st, sgNameKey(name), &gid)
	return gid, err
}

// delSGDataEntry deletes the SyncGroup data entry.
func delSGDataEntry(tx store.StoreReadWriter, gid GroupId) error {
	return tx.Delete([]byte(sgDataKey(gid)))
}

// delSGNameEntry deletes the SyncGroup name to ID mapping.
func delSGNameEntry(tx store.StoreReadWriter, name string) error {
	return tx.Delete([]byte(sgNameKey(name)))
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
