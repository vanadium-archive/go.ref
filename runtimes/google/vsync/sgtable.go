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
//   * peerSGs: an inverted index of SyncGroup RootOIDs to sets of peer
//              SyncGroup IDs, i.e. SyncGroups defined on the same root
//              path in the Store (RootOID)

import (
	"errors"
	"fmt"

	"veyron/services/syncgroup"

	"veyron2/storage"
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
	peerSGs map[storage.ID]sgSet   // in-memory tracking of peer SyncGroups per RootOID
}

type syncGroupData struct {
	SrvInfo   syncgroup.SyncGroupInfo // SyncGroup info from SyncGroupServer
	LocalPath string                  // local path of the SyncGroup in the Store
}

type memberInfo struct {
	gids map[syncgroup.ID]*memberMetaData // map of SyncGroup IDs joined and their metadata
}

type memberMetaData struct {
	metaData syncgroup.JoinerMetaData // joiner metadata at the SyncGroup server
	identity string                   // joiner security identity
}

type sgSet map[syncgroup.ID]struct{} // a set of SyncGroups

// openSyncGroupTable opens or creates a syncGroupTable for the given filename.
func openSyncGroupTable(filename string) (*syncGroupTable, error) {
	// Open the file and create it if it does not exist.
	// Also initialize the store and its tables.
	db, tbls, err := kvdbOpen(filename, []string{"data", "names"})
	if err != nil {
		return nil, err
	}

	s := &syncGroupTable{
		fname:   filename,
		store:   db,
		sgData:  tbls[0],
		sgNames: tbls[1],
		members: make(map[string]*memberInfo),
		peerSGs: make(map[storage.ID]sgSet),
	}

	// Reconstruct the in-memory tracking maps by iterating over the SyncGroups.
	// This is needed when an existing SyncGroup Table file is re-opened.
	s.sgData.keyIter(func(gidStr string) {
		// Get the SyncGroup data given the group ID in string format (as the data table key).
		gid, err := syncgroup.ParseID(gidStr)
		if err != nil {
			return
		}

		data, err := s.getSyncGroupByID(gid)
		if err != nil {
			return
		}

		// Add all SyncGroup members to the members inverted index.
		s.addAllMembers(data)

		// Add the SyncGroup ID to its peer SyncGroup set based on the RootOID.
		s.addPeerSyncGroup(gid, data.SrvInfo.RootOID)
	})

	return s, nil
}

// close closes the syncGroupTable and invalidates its structure.
func (s *syncGroupTable) close() {
	if s.store != nil {
		s.store.close() // this also closes the tables
	}
	*s = syncGroupTable{} // zero out the structure
}

// flush flushes the syncGroupTable store to disk.
func (s *syncGroupTable) flush() {
	if s.store != nil {
		s.store.flush()
	}
}

// compact compacts the kvdb file of the syncGroupTable.
func (s *syncGroupTable) compact() error {
	if s.store == nil {
		return errBadSGTable
	}
	db, tbls, err := s.store.compact(s.fname, []string{"data", "names"})
	if err != nil {
		return err
	}
	s.store = db
	s.sgData = tbls[0]
	s.sgNames = tbls[1]
	return nil
}

// addGroup adds a new SyncGroup given its information.
func (s *syncGroupTable) addSyncGroup(sgData *syncGroupData) error {
	if s.store == nil {
		return errBadSGTable
	}
	if sgData == nil {
		return errors.New("group information not specified")
	}
	gid, name := sgData.SrvInfo.SGOID, sgData.SrvInfo.Name
	if name == "" {
		return errors.New("group name not specified")
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

	s.addAllMembers(sgData)
	s.addPeerSyncGroup(gid, sgData.SrvInfo.RootOID)
	return nil
}

// getSyncGroupID retrieves the SyncGroup ID given its name.
func (s *syncGroupTable) getSyncGroupID(name string) (syncgroup.ID, error) {
	return s.getSGNameEntry(name)
}

// getSyncGroupName retrieves the SyncGroup name given its ID.
func (s *syncGroupTable) getSyncGroupName(gid syncgroup.ID) (string, error) {
	data, err := s.getSyncGroupByID(gid)
	if err != nil {
		return "", err
	}

	return data.SrvInfo.Name, nil
}

// getSyncGroupByID retrieves the SyncGroup given its ID.
func (s *syncGroupTable) getSyncGroupByID(gid syncgroup.ID) (*syncGroupData, error) {
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
	if data.SrvInfo.Name == "" {
		return errors.New("group name not specified")
	}
	if len(data.SrvInfo.Joiners) == 0 {
		return errors.New("group has no joiners")
	}

	oldData, err := s.getSyncGroupByName(data.SrvInfo.Name)
	if err != nil {
		return err
	}

	if data.SrvInfo.SGOID != oldData.SrvInfo.SGOID {
		return fmt.Errorf("cannot change ID of SyncGroup name %s", data.SrvInfo.Name)
	}
	if data.SrvInfo.RootOID != oldData.SrvInfo.RootOID {
		return fmt.Errorf("cannot change root ID of SyncGroup name %s", data.SrvInfo.Name)
	}

	// Get the old set of SyncGroup joiners and diff it with the new set.
	// Add all the current members because this inserts the new members and
	// updates the metadata of the existing ones (addMember() is like a "put").
	// Delete the members that are no longer part of the SyncGroup.
	gid := oldData.SrvInfo.SGOID
	newJoiners, oldJoiners := data.SrvInfo.Joiners, oldData.SrvInfo.Joiners

	for member, memberData := range newJoiners {
		s.addMember(member.Name, gid, member.Identity, memberData)
	}

	for member := range oldJoiners {
		if _, ok := newJoiners[member]; !ok {
			s.delMember(member.Name, gid)
		}
	}

	return s.setSGDataEntry(gid, data)
}

// delSyncGroupByID deletes the SyncGroup given its ID.
func (s *syncGroupTable) delSyncGroupByID(gid syncgroup.ID) error {
	data, err := s.getSyncGroupByID(gid)
	if err != nil {
		return err
	}
	if err = s.delSGNameEntry(data.SrvInfo.Name); err != nil {
		return err
	}

	s.delAllMembers(data)
	s.delPeerSyncGroup(gid, data.SrvInfo.RootOID)
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
func (s *syncGroupTable) addMember(member string, gid syncgroup.ID, identity string, metadata syncgroup.JoinerMetaData) {
	if s.store == nil {
		return
	}

	info, ok := s.members[member]
	if !ok {
		info = &memberInfo{gids: make(map[syncgroup.ID]*memberMetaData)}
		s.members[member] = info
	}

	info.gids[gid] = &memberMetaData{metaData: metadata, identity: identity}
}

// delMember removes a (member, group ID) entry from the in-memory structure
// that indexes SyncGroup memberships based on member names.
func (s *syncGroupTable) delMember(member string, gid syncgroup.ID) {
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
	}
}

// addAllMembers inserts all members of a SyncGroup in the in-memory structure
// that indexes SyncGroup memberships based on member names.
func (s *syncGroupTable) addAllMembers(data *syncGroupData) {
	if s.store == nil || data == nil {
		return
	}

	gid := data.SrvInfo.SGOID
	for member, memberData := range data.SrvInfo.Joiners {
		s.addMember(member.Name, gid, member.Identity, memberData)
	}
}

// delAllMembers removes all members of a SyncGroup from the in-memory structure
// that indexes SyncGroup memberships based on member names.
func (s *syncGroupTable) delAllMembers(data *syncGroupData) {
	if s.store == nil || data == nil {
		return
	}

	gid := data.SrvInfo.SGOID
	for member := range data.SrvInfo.Joiners {
		s.delMember(member.Name, gid)
	}
}

// addPeerSyncGroup inserts the group ID into the in-memory set of peer SyncGroups
// that use the same RootOID in the Store.
func (s *syncGroupTable) addPeerSyncGroup(gid syncgroup.ID, rootOID storage.ID) {
	if s.store == nil {
		return
	}

	peers, ok := s.peerSGs[rootOID]
	if !ok {
		peers = make(sgSet)
		s.peerSGs[rootOID] = peers
	}

	peers[gid] = struct{}{}
}

// delPeerSyncGroup removes the group ID from the in-memory set of peer SyncGroups
// that use the same RootOID in the Store.
func (s *syncGroupTable) delPeerSyncGroup(gid syncgroup.ID, rootOID storage.ID) {
	if s.store == nil {
		return
	}

	peers, ok := s.peerSGs[rootOID]
	if !ok {
		return
	}

	delete(peers, gid)
	if len(peers) == 0 {
		delete(s.peerSGs, rootOID)
	}
}

// getPeerSyncGroups returns the set of peer SyncGroups for a given SyncGroup.
// The given SyncGroup ID is included in that set.
func (s *syncGroupTable) getPeerSyncGroups(gid syncgroup.ID) (sgSet, error) {
	if s.store == nil {
		return sgSet{}, errBadSGTable
	}

	data, err := s.getSyncGroupByID(gid)
	if err != nil {
		return sgSet{}, err
	}

	return s.peerSGs[data.SrvInfo.RootOID], nil
}

// Low-level functions to access the tables in the K/V DB.
// They directly access the table entries without tracking their relationships.

// sgDataKey returns the key used to access the SyncGroup data in the DB.
func sgDataKey(gid syncgroup.ID) string {
	return gid.String()
}

// hasSGDataEntry returns true if the SyncGroup data entry exists in the DB.
func (s *syncGroupTable) hasSGDataEntry(gid syncgroup.ID) bool {
	if s.store == nil {
		return false
	}
	key := sgDataKey(gid)
	return s.sgData.hasKey(key)
}

// setSGDataEntry stores the SyncGroup data in the DB.
func (s *syncGroupTable) setSGDataEntry(gid syncgroup.ID, data *syncGroupData) error {
	if s.store == nil {
		return errBadSGTable
	}
	key := sgDataKey(gid)
	return s.sgData.set(key, data)
}

// getSGDataEntry retrieves from the DB the SyncGroup data for a given group ID.
func (s *syncGroupTable) getSGDataEntry(gid syncgroup.ID) (*syncGroupData, error) {
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
func (s *syncGroupTable) delSGDataEntry(gid syncgroup.ID) error {
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
func (s *syncGroupTable) setSGNameEntry(name string, gid syncgroup.ID) error {
	if s.store == nil {
		return errBadSGTable
	}
	key := sgNameKey(name)
	return s.sgNames.set(key, gid)
}

// getSGNameEntry retrieves the SyncGroup name to ID mapping from the DB.
func (s *syncGroupTable) getSGNameEntry(name string) (syncgroup.ID, error) {
	var gid syncgroup.ID
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
