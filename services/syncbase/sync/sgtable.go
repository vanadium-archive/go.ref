// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// The SyncGroup Table stores the group information in a K/V DB.  It also
// maintains an index to provide access by SyncGroup ID or name.
//
// The SyncGroup info is fetched from the SyncGroup server by the create or
// join operations, and is regularly updated after that.
//
// The DB contains two tables persisted to disk (data, names) and one
// in-memory (ephemeral) map (members):
//   * data:    one entry per SyncGroup ID containing the SyncGroup data
//   * names:   one entry per SyncGroup name pointing to its SyncGroup ID
//   * members: an inverted index of SyncGroup members to SyncGroup IDs
//              built from the list of SyncGroup joiners

import (
	"errors"
	"fmt"
	"path"
	"strconv"

	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/stats"
)

var (
	errBadSGTable = errors.New("invalid SyncGroup Table")
)

type syncGroupTable struct {
	fname   string                 // file pathname
	store   *kvdb                  // underlying K/V store
	sgData  *kvtable               // pointer to "data" table in the kvdb
	sgNames *kvtable               // pointer to "names" table in the kvdb
	members map[string]*memberInfo // in-memory tracking of SyncGroup member info

	// SyncGroup Table stats
	numSGs     *stats.Integer // number of SyncGroups
	numMembers *stats.Integer // number of Sync members
}

type syncGroupData struct {
	SrvInfo   SyncGroupInfo // SyncGroup info from SyncGroupServer
	LocalPath string        // local path of the SyncGroup in the Store
}

type memberInfo struct {
	gids map[GroupId]*memberMetaData // map of SyncGroup IDs joined and their metadata
}

type memberMetaData struct {
	metaData JoinerMetaData // joiner metadata at the SyncGroup server
}

type sgSet map[GroupId]struct{} // a set of SyncGroups

// strToGroupId converts a SyncGroup ID in string format to an GroupId.
func strToGroupId(str string) (GroupId, error) {
	id, err := strconv.ParseUint(str, 10, 64)
	if err != nil {
		return NoGroupId, err
	}
	return GroupId(id), nil
}

// openSyncGroupTable opens or creates a syncGroupTable for the given filename.
func openSyncGroupTable(filename string) (*syncGroupTable, error) {
	// Open the file and create it if it does not exist.
	// Also initialize the store and its tables.
	db, tbls, err := kvdbOpen(filename, []string{"data", "names"})
	if err != nil {
		return nil, err
	}

	s := &syncGroupTable{
		fname:      filename,
		store:      db,
		sgData:     tbls[0],
		sgNames:    tbls[1],
		members:    make(map[string]*memberInfo),
		numSGs:     stats.NewInteger(statsNumSyncGroup),
		numMembers: stats.NewInteger(statsNumMember),
	}

	// Reconstruct the in-memory tracking maps by iterating over the SyncGroups.
	// This is needed when an existing SyncGroup Table file is re-opened.
	s.sgData.keyIter(func(gidStr string) {
		// Get the SyncGroup data given the group ID in string format (as the data table key).
		gid, err := strToGroupId(gidStr)
		if err != nil {
			return
		}

		data, err := s.getSyncGroupByID(gid)
		if err != nil {
			return
		}

		s.numSGs.Incr(1)

		// Add all SyncGroup members to the members inverted index.
		s.addAllMembers(data)
	})

	return s, nil
}

// close closes the syncGroupTable and invalidates its structure.
func (s *syncGroupTable) close() {
	if s.store != nil {
		s.store.close() // this also closes the tables
		stats.Delete(statsNumSyncGroup)
		stats.Delete(statsNumMember)
	}
	*s = syncGroupTable{} // zero out the structure
}

// flush flushes the syncGroupTable store to disk.
func (s *syncGroupTable) flush() {
	if s.store != nil {
		s.store.flush()
	}
}

// addSyncGroup adds a new SyncGroup given its information.
func (s *syncGroupTable) addSyncGroup(sgData *syncGroupData) error {
	if s.store == nil {
		return errBadSGTable
	}
	if sgData == nil {
		return errors.New("group information not specified")
	}
	gid, name := sgData.SrvInfo.Id, path.Join(sgData.SrvInfo.ServerName, sgData.SrvInfo.GroupName)
	if name == "" {
		return errors.New("group name not specified")
	}
	if sgData.LocalPath == "" {
		return errors.New("group local path not specified")
	}
	if len(sgData.SrvInfo.Joiners) == 0 {
		return errors.New("group has no joiners")
	}

	if s.hasSGDataEntry(gid) {
		return fmt.Errorf("group %d already exists", gid)
	}
	if s.hasSGNameEntry(name) {
		return fmt.Errorf("group name %s already exists", name)
	}

	// Add the group name and data entries.
	if err := s.setSGNameEntry(name, gid); err != nil {
		return err
	}

	if err := s.setSGDataEntry(gid, sgData); err != nil {
		s.delSGNameEntry(name)
		return err
	}

	s.numSGs.Incr(1)
	s.addAllMembers(sgData)
	return nil
}

// getSyncGroupID retrieves the SyncGroup ID given its name.
func (s *syncGroupTable) getSyncGroupID(name string) (GroupId, error) {
	return s.getSGNameEntry(name)
}

// getSyncGroupName retrieves the SyncGroup name given its ID.
func (s *syncGroupTable) getSyncGroupName(gid GroupId) (string, error) {
	data, err := s.getSyncGroupByID(gid)
	if err != nil {
		return "", err
	}

	return path.Join(data.SrvInfo.ServerName, data.SrvInfo.GroupName), nil
}

// getSyncGroupByID retrieves the SyncGroup given its ID.
func (s *syncGroupTable) getSyncGroupByID(gid GroupId) (*syncGroupData, error) {
	return s.getSGDataEntry(gid)
}

// getSyncGroupByName retrieves the SyncGroup given its name.
func (s *syncGroupTable) getSyncGroupByName(name string) (*syncGroupData, error) {
	gid, err := s.getSyncGroupID(name)
	if err != nil {
		return nil, err
	}
	return s.getSyncGroupByID(gid)
}

// updateSyncGroup updates the SyncGroup data.
func (s *syncGroupTable) updateSyncGroup(data *syncGroupData) error {
	if s.store == nil {
		return errBadSGTable
	}
	if data == nil {
		return errors.New("SyncGroup data not specified")
	}
	if data.SrvInfo.GroupName == "" {
		return errors.New("group name not specified")
	}
	if len(data.SrvInfo.Joiners) == 0 {
		return errors.New("group has no joiners")
	}

	fullGroupName := path.Join(data.SrvInfo.ServerName, data.SrvInfo.GroupName)
	oldData, err := s.getSyncGroupByName(fullGroupName)
	if err != nil {
		return err
	}

	if data.SrvInfo.Id != oldData.SrvInfo.Id {
		return fmt.Errorf("cannot change ID of SyncGroup name %s", fullGroupName)
	}
	if data.LocalPath == "" {
		data.LocalPath = oldData.LocalPath
	} else if data.LocalPath != oldData.LocalPath {
		return fmt.Errorf("cannot change local path of SyncGroup name %s", fullGroupName)
	}

	// Get the old set of SyncGroup joiners and diff it with the new set.
	// Add all the current members because this inserts the new members and
	// updates the metadata of the existing ones (addMember() is like a "put").
	// Delete the members that are no longer part of the SyncGroup.
	gid := oldData.SrvInfo.Id
	newJoiners, oldJoiners := data.SrvInfo.Joiners, oldData.SrvInfo.Joiners

	for member, memberData := range newJoiners {
		s.addMember(member, gid, memberData)
	}

	for member := range oldJoiners {
		if _, ok := newJoiners[member]; !ok {
			s.delMember(member, gid)
		}
	}

	return s.setSGDataEntry(gid, data)
}

// delSyncGroupByID deletes the SyncGroup given its ID.
func (s *syncGroupTable) delSyncGroupByID(gid GroupId) error {
	data, err := s.getSyncGroupByID(gid)
	if err != nil {
		return err
	}
	if err = s.delSGNameEntry(path.Join(data.SrvInfo.ServerName, data.SrvInfo.GroupName)); err != nil {
		return err
	}

	s.numSGs.Incr(-1)
	s.delAllMembers(data)
	return s.delSGDataEntry(gid)
}

// delSyncGroupByName deletes the SyncGroup given its name.
func (s *syncGroupTable) delSyncGroupByName(name string) error {
	gid, err := s.getSyncGroupID(name)
	if err != nil {
		return err
	}

	return s.delSyncGroupByID(gid)
}

// getAllSyncGroupNames returns the names of all SyncGroups.
func (s *syncGroupTable) getAllSyncGroupNames() ([]string, error) {
	if s.store == nil {
		return nil, errBadSGTable
	}

	names := make([]string, 0)

	err := s.sgNames.keyIter(func(name string) {
		names = append(names, name)
	})

	if err != nil {
		return nil, err
	}
	return names, nil
}

// getMembers returns all SyncGroup members and the count of SyncGroups each one joined.
func (s *syncGroupTable) getMembers() (map[string]uint32, error) {
	if s.store == nil {
		return nil, errBadSGTable
	}

	members := make(map[string]uint32)
	for member, info := range s.members {
		members[member] = uint32(len(info.gids))
	}

	return members, nil
}

// getMemberInfo returns SyncGroup information for a given member.
func (s *syncGroupTable) getMemberInfo(member string) (*memberInfo, error) {
	if s.store == nil {
		return nil, errBadSGTable
	}

	info, ok := s.members[member]
	if !ok {
		return nil, fmt.Errorf("unknown member: %s", member)
	}

	return info, nil
}

// addMember inserts or updates a (member, group ID) entry in the in-memory
// structure that indexes SyncGroup memberships based on member names and stores
// in it the member's joiner metadata.
func (s *syncGroupTable) addMember(member string, gid GroupId, metadata JoinerMetaData) {
	if s.store == nil {
		return
	}

	info, ok := s.members[member]
	if !ok {
		info = &memberInfo{gids: make(map[GroupId]*memberMetaData)}
		s.members[member] = info
		s.numMembers.Incr(1)
	}

	info.gids[gid] = &memberMetaData{metaData: metadata}
}

// delMember removes a (member, group ID) entry from the in-memory structure
// that indexes SyncGroup memberships based on member names.
func (s *syncGroupTable) delMember(member string, gid GroupId) {
	if s.store == nil {
		return
	}

	info, ok := s.members[member]
	if !ok {
		return
	}

	delete(info.gids, gid)
	if len(info.gids) == 0 {
		delete(s.members, member)
		s.numMembers.Incr(-1)
	}
}

// addAllMembers inserts all members of a SyncGroup in the in-memory structure
// that indexes SyncGroup memberships based on member names.
func (s *syncGroupTable) addAllMembers(data *syncGroupData) {
	if s.store == nil || data == nil {
		return
	}

	gid := data.SrvInfo.Id
	for member, memberData := range data.SrvInfo.Joiners {
		s.addMember(member, gid, memberData)
	}
}

// delAllMembers removes all members of a SyncGroup from the in-memory structure
// that indexes SyncGroup memberships based on member names.
func (s *syncGroupTable) delAllMembers(data *syncGroupData) {
	if s.store == nil || data == nil {
		return
	}

	gid := data.SrvInfo.Id
	for member := range data.SrvInfo.Joiners {
		s.delMember(member, gid)
	}
}

// Low-level functions to access the tables in the K/V DB.
// They directly access the table entries without tracking their relationships.

// sgDataKey returns the key used to access the SyncGroup data in the DB.
func sgDataKey(gid GroupId) string {
	return fmt.Sprintf("%d", gid)
}

// hasSGDataEntry returns true if the SyncGroup data entry exists in the DB.
func (s *syncGroupTable) hasSGDataEntry(gid GroupId) bool {
	if s.store == nil {
		return false
	}
	key := sgDataKey(gid)
	return s.sgData.hasKey(key)
}

// setSGDataEntry stores the SyncGroup data in the DB.
func (s *syncGroupTable) setSGDataEntry(gid GroupId, data *syncGroupData) error {
	if s.store == nil {
		return errBadSGTable
	}
	key := sgDataKey(gid)
	return s.sgData.set(key, data)
}

// getSGDataEntry retrieves from the DB the SyncGroup data for a given group ID.
func (s *syncGroupTable) getSGDataEntry(gid GroupId) (*syncGroupData, error) {
	if s.store == nil {
		return nil, errBadSGTable
	}
	var data syncGroupData
	key := sgDataKey(gid)
	if err := s.sgData.get(key, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

// delSGDataEntry deletes the SyncGroup data from the DB.
func (s *syncGroupTable) delSGDataEntry(gid GroupId) error {
	if s.store == nil {
		return errBadSGTable
	}
	key := sgDataKey(gid)
	return s.sgData.del(key)
}

// sgNameKey returns the key used to access the SyncGroup name in the DB.
func sgNameKey(name string) string {
	return name
}

// hasSGNameEntry returns true if the SyncGroup name entry exists in the DB.
func (s *syncGroupTable) hasSGNameEntry(name string) bool {
	if s.store == nil {
		return false
	}
	key := sgNameKey(name)
	return s.sgNames.hasKey(key)
}

// setSGNameEntry stores the SyncGroup name to ID mapping in the DB.
func (s *syncGroupTable) setSGNameEntry(name string, gid GroupId) error {
	if s.store == nil {
		return errBadSGTable
	}
	key := sgNameKey(name)
	return s.sgNames.set(key, gid)
}

// getSGNameEntry retrieves the SyncGroup name to ID mapping from the DB.
func (s *syncGroupTable) getSGNameEntry(name string) (GroupId, error) {
	var gid GroupId
	if s.store == nil {
		return gid, errBadSGTable
	}
	key := sgNameKey(name)
	err := s.sgNames.get(key, &gid)
	return gid, err
}

// delSGNameEntry deletes the SyncGroup name to ID mapping from the DB.
func (s *syncGroupTable) delSGNameEntry(name string) error {
	if s.store == nil {
		return errBadSGTable
	}
	key := sgNameKey(name)
	return s.sgNames.del(key)
}

// dump writes to the log file information on all SyncGroups.
func (s *syncGroupTable) dump() {
	if s.store == nil {
		return
	}

	s.sgData.keyIter(func(gidStr string) {
		// Get the SyncGroup data given the group ID in string format (as the data table key).
		gid, err := strToGroupId(gidStr)
		if err != nil {
			return
		}

		data, err := s.getSyncGroupByID(gid)
		if err != nil {
			return
		}

		members := make([]string, 0, len(data.SrvInfo.Joiners))
		for joiner := range data.SrvInfo.Joiners {
			members = append(members, joiner)
		}
		vlog.VI(1).Infof("DUMP: SyncGroup %s: id %v, path %s, members: %s",
			path.Join(data.SrvInfo.ServerName, data.SrvInfo.GroupName),
			gid, data.LocalPath, members)
	})
}
