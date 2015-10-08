// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// SyncGroup management and storage in Syncbase.  Handles the lifecycle
// of SyncGroups (create, join, leave, etc.) and their persistence as
// sync metadata in the application databases.  Provides helper functions
// to the higher levels of sync (Initiator, Watcher) to get membership
// information and map key/value changes to their matching SyncGroups.

// TODO(hpucha): Add high level commentary about the logic behind create/join
// etc.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	wire "v.io/v23/services/syncbase/nosql"
	pubutil "v.io/v23/syncbase/util"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/lib/vlog"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/server/watchable"
	"v.io/x/ref/services/syncbase/store"
)

var (
	// memberViewTTL is the shelf-life of the aggregate view of SyncGroup members.
	memberViewTTL = 2 * time.Second
)

////////////////////////////////////////////////////////////
// SyncGroup management internal to Syncbase.

// memberView holds an aggregated view of all SyncGroup members across
// databases. The view is not coherent, it gets refreshed according to a
// configured TTL and not (coherently) when SyncGroup membership is updated in
// the various databases. It is needed by the sync Initiator, which must select
// a peer to contact from a global view of all SyncGroup members gathered from
// all databases. This is why a slightly stale view is acceptable.
// The members are identified by their Vanadium names (map keys).
type memberView struct {
	expiration time.Time
	members    map[string]*memberInfo
}

// memberInfo holds the member metadata for each SyncGroup this member belongs
// to within each App/Database (i.e. global database name). It's a mapping of
// global DB names to sets of SyncGroup member information. It also maintains
// all the mount table candidates that could be used to reach this peer, learned
// from the SyncGroup metadata.
type memberInfo struct {
	db2sg    map[string]sgMemberInfo
	mtTables map[string]struct{}
}

// sgMemberInfo maps SyncGroups to their member metadata.
type sgMemberInfo map[interfaces.GroupId]wire.SyncGroupMemberInfo

// newSyncGroupVersion generates a random SyncGroup version ("etag").
func newSyncGroupVersion() string {
	return fmt.Sprintf("%x", rand64())
}

// newSyncGroupId generates a random SyncGroup ID.
func newSyncGroupId() interfaces.GroupId {
	id := interfaces.NoGroupId
	for id == interfaces.NoGroupId {
		id = interfaces.GroupId(rand64())
	}
	return id
}

// verifySyncGroup verifies if a SyncGroup struct is well-formed.
// TODO(rdaoud): define verrors for all ErrBadArg cases.
func verifySyncGroup(ctx *context.T, sg *interfaces.SyncGroup) error {
	if sg == nil {
		return verror.New(verror.ErrBadArg, ctx, "group information not specified")
	}
	if sg.Name == "" {
		return verror.New(verror.ErrBadArg, ctx, "group name not specified")
	}
	if sg.AppName == "" {
		return verror.New(verror.ErrBadArg, ctx, "app name not specified")
	}
	if sg.DbName == "" {
		return verror.New(verror.ErrBadArg, ctx, "db name not specified")
	}
	if sg.Creator == "" {
		return verror.New(verror.ErrBadArg, ctx, "creator id not specified")
	}
	if sg.Id == interfaces.NoGroupId {
		return verror.New(verror.ErrBadArg, ctx, "group id not specified")
	}
	if sg.SpecVersion == "" {
		return verror.New(verror.ErrBadArg, ctx, "group version not specified")
	}
	if len(sg.Joiners) == 0 {
		return verror.New(verror.ErrBadArg, ctx, "group has no joiners")
	}
	return verifySyncGroupSpec(ctx, &sg.Spec)
}

// verifySyncGroupSpec verifies if a SyncGroupSpec is well-formed.
func verifySyncGroupSpec(ctx *context.T, spec *wire.SyncGroupSpec) error {
	if spec == nil {
		return verror.New(verror.ErrBadArg, ctx, "group spec not specified")
	}
	if len(spec.Prefixes) == 0 {
		return verror.New(verror.ErrBadArg, ctx, "group has no prefixes specified")
	}

	// Duplicate prefixes are not allowed.
	prefixes := make(map[string]bool, len(spec.Prefixes))
	for _, p := range spec.Prefixes {
		if !pubutil.ValidTableName(p.TableName) {
			return verror.New(verror.ErrBadArg, ctx, fmt.Sprintf("group has a SyncGroupPrefix with invalid table name %q", p.TableName))
		}
		if p.RowPrefix != "" && !pubutil.ValidRowKey(p.RowPrefix) {
			return verror.New(verror.ErrBadArg, ctx, fmt.Sprintf("group has a SyncGroupPrefix with invalid row prefix %q", p.RowPrefix))
		}
		prefixes[toTableRowPrefixStr(p)] = true
	}
	if len(prefixes) != len(spec.Prefixes) {
		return verror.New(verror.ErrBadArg, ctx, "group has duplicate prefixes specified")
	}
	return nil
}

// samePrefixes returns true if the two sets of prefixes are the same.
func samePrefixes(pfx1, pfx2 []wire.SyncGroupPrefix) bool {
	pfxMap := make(map[string]uint8)
	for _, p := range pfx1 {
		pfxMap[toTableRowPrefixStr(p)] |= 0x01
	}
	for _, p := range pfx2 {
		pfxMap[toTableRowPrefixStr(p)] |= 0x02
	}
	for _, mask := range pfxMap {
		if mask != 0x03 {
			return false
		}
	}
	return true
}

// addSyncGroup adds a new SyncGroup given its version and information.  This
// also includes creating a DAG node entry and updating the DAG head.  If the
// caller is the creator of the SyncGroup, a local log record is also created
// using the given server ID and gen and pos counters to index the log record.
// Otherwise, it's a joiner case and the SyncGroup is put in a pending state
// (waiting for its full metadata to be synchronized) and the log record is
// skipped, delaying its creation till the Initiator does p2p sync.
func (s *syncService) addSyncGroup(ctx *context.T, tx store.Transaction, version string, creator bool, remotePublisher string, genvec interfaces.PrefixGenVector, servId, gen, pos uint64, sg *interfaces.SyncGroup) error {
	// Verify the SyncGroup information before storing it since it may have
	// been received from a remote peer.
	if err := verifySyncGroup(ctx, sg); err != nil {
		return err
	}

	// Add the group name and ID entries.
	if ok, err := hasSGNameEntry(tx, sg.Name); err != nil {
		return err
	} else if ok {
		return verror.New(verror.ErrExist, ctx, "group name already exists")
	}
	if ok, err := hasSGIdEntry(tx, sg.Id); err != nil {
		return err
	} else if ok {
		return verror.New(verror.ErrExist, ctx, "group id already exists")
	}

	state := sgLocalState{
		RemotePublisher: remotePublisher,
		SyncPending:     !creator,
		PendingGenVec:   genvec,
	}
	if remotePublisher == "" {
		state.NumLocalJoiners = 1
	}

	if err := setSGNameEntry(ctx, tx, sg.Name, sg.Id); err != nil {
		return err
	}
	if err := setSGIdEntry(ctx, tx, sg.Id, &state); err != nil {
		return err
	}

	// Add the SyncGroup versioned data entry.
	if ok, err := hasSGDataEntry(tx, sg.Id, version); err != nil {
		return err
	} else if ok {
		return verror.New(verror.ErrExist, ctx, "group id version already exists")
	}

	return s.updateSyncGroupVersioning(ctx, tx, version, creator, servId, gen, pos, sg)
}

// updateSyncGroupVersioning updates the per-version information of a SyncGroup.
// It writes a new versioned copy of the SyncGroup data entry, a new DAG node,
// and updates the DAG head.  Optionally, it also writes a new local log record
// using the given server ID and gen and pos counters to index it.  The caller
// can provide the version number to use otherwise, if NoVersion is given, a new
// version is generated by the function.
// TODO(rdaoud): hook SyncGroup mutations (and deletions) to the watch log so
// apps can monitor SG changes as well.
func (s *syncService) updateSyncGroupVersioning(ctx *context.T, tx store.Transaction, version string, withLog bool, servId, gen, pos uint64, sg *interfaces.SyncGroup) error {
	if version == NoVersion {
		version = newSyncGroupVersion()
	}

	oid := sgOID(sg.Id)

	// Add the SyncGroup versioned data entry.
	if err := setSGDataEntryByOID(ctx, tx, oid, version, sg); err != nil {
		return err
	}

	var parents []string
	if head, err := getHead(ctx, tx, oid); err == nil {
		parents = []string{head}
	} else if verror.ErrorID(err) != verror.ErrNoExist.ID {
		return err
	}

	// Add a sync log record for the SyncGroup if needed.
	logKey := ""
	if withLog {
		if err := addSyncGroupLogRec(ctx, tx, oid, version, parents, servId, gen, pos); err != nil {
			return err
		}
		logKey = logRecKey(oid, servId, gen)
	}

	// Add the SyncGroup to the DAG.
	if err := s.addNode(ctx, tx, oid, version, logKey, false, parents, NoBatchId, nil); err != nil {
		return err
	}
	return setHead(ctx, tx, oid, version)
}

// addSyncGroupLogRec adds a new local log record for a SyncGroup.
func addSyncGroupLogRec(ctx *context.T, tx store.Transaction, oid, version string, parents []string, servId, gen, pos uint64) error {
	rec := &localLogRec{
		Metadata: interfaces.LogRecMetadata{
			ObjId:   oid,
			CurVers: version,
			Parents: parents,
			Delete:  false,
			UpdTime: watchable.GetStoreTime(ctx, tx),
			Id:      servId,
			Gen:     gen,
			RecType: interfaces.NodeRec,
			BatchId: NoBatchId,
		},
		Pos: pos,
	}

	return putLogRec(ctx, tx, oid, rec)
}

// getSyncGroupId retrieves the SyncGroup ID given its name.
func getSyncGroupId(ctx *context.T, st store.StoreReader, name string) (interfaces.GroupId, error) {
	return getSGNameEntry(ctx, st, name)
}

// getSyncGroupVersion retrieves the current version of the SyncGroup.
func getSyncGroupVersion(ctx *context.T, st store.StoreReader, gid interfaces.GroupId) (string, error) {
	return getHead(ctx, st, sgOID(gid))
}

// getSyncGroupById retrieves the SyncGroup given its ID.
func getSyncGroupById(ctx *context.T, st store.StoreReader, gid interfaces.GroupId) (*interfaces.SyncGroup, error) {
	version, err := getSyncGroupVersion(ctx, st, gid)
	if err != nil {
		return nil, err
	}
	return getSGDataEntry(ctx, st, gid, version)
}

// getSyncGroupByName retrieves the SyncGroup given its name.
func getSyncGroupByName(ctx *context.T, st store.StoreReader, name string) (*interfaces.SyncGroup, error) {
	gid, err := getSyncGroupId(ctx, st, name)
	if err != nil {
		return nil, err
	}
	return getSyncGroupById(ctx, st, gid)
}

// delSyncGroupById deletes the SyncGroup given its ID.
func delSyncGroupById(ctx *context.T, tx store.Transaction, gid interfaces.GroupId) error {
	sg, err := getSyncGroupById(ctx, tx, gid)
	if err != nil {
		return err
	}
	return delSyncGroupByName(ctx, tx, sg.Name)
}

// delSyncGroupByName deletes the SyncGroup given its name.
func delSyncGroupByName(ctx *context.T, tx store.Transaction, name string) error {
	// Get the SyncGroup ID and current version.
	gid, err := getSyncGroupId(ctx, tx, name)
	if err != nil {
		return err
	}
	version, err := getSyncGroupVersion(ctx, tx, gid)
	if err != nil {
		return err
	}

	// Delete the name and ID entries.
	if err := delSGNameEntry(ctx, tx, name); err != nil {
		return err
	}
	if err := delSGIdEntry(ctx, tx, gid); err != nil {
		return err
	}

	// Delete all versioned SyncGroup data entries (same versions as DAG
	// nodes).  This is done separately from pruning the DAG nodes because
	// some nodes may have no log record pointing back to the SyncGroup data
	// entries (loose coupling to support the pending SyncGroup state).
	oid := sgOID(gid)
	err = forEachAncestor(ctx, tx, oid, []string{version}, func(v string, nd *dagNode) error {
		return delSGDataEntry(ctx, tx, gid, v)
	})
	if err != nil {
		return err
	}

	// Delete all DAG nodes and log records.
	bset := newBatchPruning()
	err = prune(ctx, tx, oid, NoVersion, bset, func(ctx *context.T, tx store.Transaction, lr string) error {
		if lr != "" {
			return util.Delete(ctx, tx, lr)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return pruneDone(ctx, tx, bset)
}

// refreshMembersIfExpired updates the aggregate view of SyncGroup members
// across databases if the view has expired.
// TODO(rdaoud): track dirty apps/dbs since the last refresh and incrementally
// update the membership view for them instead of always scanning all of them.
func (s *syncService) refreshMembersIfExpired(ctx *context.T) {
	view := s.allMembers
	if view == nil {
		// The empty expiration time in Go is before "now" and treated as expired
		// below.
		view = &memberView{expiration: time.Time{}, members: nil}
		s.allMembers = view
	}

	if time.Now().Before(view.expiration) {
		return
	}

	// Create a new aggregate view of SyncGroup members across all app databases.
	newMembers := make(map[string]*memberInfo)

	s.forEachDatabaseStore(ctx, func(appName, dbName string, st store.Store) bool {
		// For each database, fetch its SyncGroup data entries by scanning their
		// prefix range.  Use a database snapshot for the scan.
		sn := st.NewSnapshot()
		defer sn.Abort()
		name := appDbName(appName, dbName)

		forEachSyncGroup(sn, func(sg *interfaces.SyncGroup) bool {
			// Add all members of this SyncGroup to the membership view.
			// A member's info is different across SyncGroups, so gather all of them.
			refreshSyncGroupMembers(sg, name, newMembers)

			return false
		})
		return false
	})

	view.members = newMembers
	view.expiration = time.Now().Add(memberViewTTL)
}

func refreshSyncGroupMembers(sg *interfaces.SyncGroup, name string, newMembers map[string]*memberInfo) {
	for member, info := range sg.Joiners {
		if _, ok := newMembers[member]; !ok {
			newMembers[member] = &memberInfo{
				db2sg:    make(map[string]sgMemberInfo),
				mtTables: make(map[string]struct{}),
			}
		}
		if _, ok := newMembers[member].db2sg[name]; !ok {
			newMembers[member].db2sg[name] = make(sgMemberInfo)
		}
		newMembers[member].db2sg[name][sg.Id] = info

		// Collect mount tables.
		for _, mt := range sg.Spec.MountTables {
			newMembers[member].mtTables[mt] = struct{}{}
		}
	}
}

// forEachSyncGroup iterates over all SyncGroups in the Database and invokes
// the callback function on each one.  The callback returns a "done" flag to
// make forEachSyncGroup() stop the iteration earlier; otherwise the function
// loops across all SyncGroups in the Database.
func forEachSyncGroup(st store.StoreReader, callback func(*interfaces.SyncGroup) bool) {
	stream := st.Scan(util.ScanPrefixArgs(sgNameKeyPrefix, ""))
	defer stream.Cancel()

	for stream.Advance() {
		var gid interfaces.GroupId
		if vom.Decode(stream.Value(nil), &gid) != nil {
			vlog.Errorf("sync: forEachSyncGroup: invalid SyncGroup ID for key %s", string(stream.Key(nil)))
			continue
		}

		sg, err := getSyncGroupById(nil, st, gid)
		if err != nil {
			vlog.Errorf("sync: forEachSyncGroup: cannot get SyncGroup %d: %v", gid, err)
			continue
		}

		if callback(sg) {
			break // done, early exit
		}
	}

	if err := stream.Err(); err != nil {
		vlog.Errorf("sync: forEachSyncGroup: scan stream error: %v", err)
	}
}

// getMembers returns all SyncGroup members and the count of SyncGroups each one
// joined.
func (s *syncService) getMembers(ctx *context.T) map[string]uint32 {
	s.allMembersLock.Lock()
	defer s.allMembersLock.Unlock()

	s.refreshMembersIfExpired(ctx)

	members := make(map[string]uint32)
	for member, info := range s.allMembers.members {
		count := 0
		for _, sgmi := range info.db2sg {
			count += len(sgmi)
		}
		members[member] = uint32(count)
	}

	return members
}

// copyMemberInfo returns a copy of the info for the requested peer.
func (s *syncService) copyMemberInfo(ctx *context.T, member string) *memberInfo {
	s.allMembersLock.RLock()
	defer s.allMembersLock.RUnlock()

	info, ok := s.allMembers.members[member]
	if !ok {
		return nil
	}

	// Make a copy.
	infoCopy := &memberInfo{
		db2sg:    make(map[string]sgMemberInfo),
		mtTables: make(map[string]struct{}),
	}
	for gdbName, sgInfo := range info.db2sg {
		infoCopy.db2sg[gdbName] = make(sgMemberInfo)
		for gid, mi := range sgInfo {
			infoCopy.db2sg[gdbName][gid] = mi
		}
	}
	for mt := range info.mtTables {
		infoCopy.mtTables[mt] = struct{}{}
	}

	return infoCopy
}

// Low-level utility functions to access DB entries without tracking their
// relationships.
// Use the functions above to manipulate SyncGroups.

var (
	// Prefixes used to store the different mappings of a SyncGroup:
	// sgNameKeyPrefix: name --> ID
	// sgIdKeyPrefix: ID --> SyncGroup local state
	// sgDataKeyPrefix: (ID, version) --> SyncGroup data (synchronized)
	//
	// Note: as with other syncable objects, the DAG "heads" table contains
	// a reference to the current SyncGroup version, and the DAG "nodes"
	// table tracks its history of mutations.
	sgNameKeyPrefix = util.JoinKeyParts(util.SyncPrefix, sgPrefix, "n")
	sgIdKeyPrefix   = util.JoinKeyParts(util.SyncPrefix, sgPrefix, "i")
	sgDataKeyPrefix = util.JoinKeyParts(util.SyncPrefix, sgDataPrefix)
)

// sgNameKey returns the key used to access the SyncGroup name entry.
func sgNameKey(name string) string {
	return util.JoinKeyParts(sgNameKeyPrefix, name)
}

// sgIdKey returns the key used to access the SyncGroup ID entry.
func sgIdKey(gid interfaces.GroupId) string {
	return util.JoinKeyParts(sgIdKeyPrefix, fmt.Sprintf("%d", gid))
}

// sgOID converts a group id into an oid string.
func sgOID(gid interfaces.GroupId) string {
	return util.JoinKeyParts(sgDataKeyPrefix, fmt.Sprintf("%d", gid))
}

// sgID is the inverse of sgOID and converts an oid string into a group id.
func sgID(oid string) (interfaces.GroupId, error) {
	parts := util.SplitKeyParts(oid)
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid sgoid %s", oid)
	}

	id, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return 0, err
	}
	return interfaces.GroupId(id), nil
}

// sgDataKey returns the key used to access a version of the SyncGroup data.
func sgDataKey(gid interfaces.GroupId, version string) string {
	return util.JoinKeyParts(sgDataKeyPrefix, fmt.Sprintf("%d", gid), version)
}

// sgDataKeyByOID returns the key used to access a version of the SyncGroup data.
func sgDataKeyByOID(oid, version string) string {
	return util.JoinKeyParts(oid, version)
}

// splitSgNameKey is the inverse of sgNameKey and returns the SyncGroup name.
func splitSgNameKey(ctx *context.T, key string) (string, error) {
	// Note that the actual SyncGroup name may contain ":" as a separator.
	// So don't split the key on the separator, instead trim its prefix.
	prefix := util.JoinKeyParts(sgNameKeyPrefix, "")
	name := strings.TrimPrefix(key, prefix)
	if name == key {
		return "", verror.New(verror.ErrInternal, ctx, "invalid sgNamekey", key)
	}
	return name, nil
}

// hasSGNameEntry returns true if the SyncGroup name entry exists.
func hasSGNameEntry(sntx store.SnapshotOrTransaction, name string) (bool, error) {
	return util.Exists(nil, sntx, sgNameKey(name))
}

// hasSGIdEntry returns true if the SyncGroup ID entry exists.
func hasSGIdEntry(sntx store.SnapshotOrTransaction, gid interfaces.GroupId) (bool, error) {
	return util.Exists(nil, sntx, sgIdKey(gid))
}

// hasSGDataEntry returns true if the SyncGroup versioned data entry exists.
func hasSGDataEntry(sntx store.SnapshotOrTransaction, gid interfaces.GroupId, version string) (bool, error) {
	return util.Exists(nil, sntx, sgDataKey(gid, version))
}

// setSGNameEntry stores the SyncGroup name entry.
func setSGNameEntry(ctx *context.T, tx store.Transaction, name string, gid interfaces.GroupId) error {
	return util.Put(ctx, tx, sgNameKey(name), gid)
}

// setSGIdEntry stores the SyncGroup ID entry.
func setSGIdEntry(ctx *context.T, tx store.Transaction, gid interfaces.GroupId, state *sgLocalState) error {
	return util.Put(ctx, tx, sgIdKey(gid), state)
}

// setSGDataEntryByOID stores the SyncGroup versioned data entry.
func setSGDataEntryByOID(ctx *context.T, tx store.Transaction, sgoid, version string, sg *interfaces.SyncGroup) error {
	return util.Put(ctx, tx, sgDataKeyByOID(sgoid, version), sg)
}

// getSGNameEntry retrieves the SyncGroup ID for a given name.
func getSGNameEntry(ctx *context.T, st store.StoreReader, name string) (interfaces.GroupId, error) {
	var gid interfaces.GroupId
	if err := util.Get(ctx, st, sgNameKey(name), &gid); err != nil {
		return interfaces.NoGroupId, err
	}
	return gid, nil
}

// getSGIdEntry retrieves the SyncGroup local state for a given group ID.
func getSGIdEntry(ctx *context.T, st store.StoreReader, gid interfaces.GroupId) (*sgLocalState, error) {
	var state sgLocalState
	if err := util.Get(ctx, st, sgIdKey(gid), &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// getSGDataEntry retrieves the SyncGroup data for a given group ID and version.
func getSGDataEntry(ctx *context.T, st store.StoreReader, gid interfaces.GroupId, version string) (*interfaces.SyncGroup, error) {
	var sg interfaces.SyncGroup
	if err := util.Get(ctx, st, sgDataKey(gid, version), &sg); err != nil {
		return nil, err
	}
	return &sg, nil
}

// getSGDataEntryByOID retrieves the SyncGroup data for a given group OID and version.
func getSGDataEntryByOID(ctx *context.T, st store.StoreReader, sgoid string, version string) (*interfaces.SyncGroup, error) {
	var sg interfaces.SyncGroup
	if err := util.Get(ctx, st, sgDataKeyByOID(sgoid, version), &sg); err != nil {
		return nil, err
	}
	return &sg, nil
}

// delSGNameEntry deletes the SyncGroup name entry.
func delSGNameEntry(ctx *context.T, tx store.Transaction, name string) error {
	return util.Delete(ctx, tx, sgNameKey(name))
}

// delSGIdEntry deletes the SyncGroup ID entry.
func delSGIdEntry(ctx *context.T, tx store.Transaction, gid interfaces.GroupId) error {
	return util.Delete(ctx, tx, sgIdKey(gid))
}

// delSGDataEntry deletes the SyncGroup versioned data entry.
func delSGDataEntry(ctx *context.T, tx store.Transaction, gid interfaces.GroupId, version string) error {
	return util.Delete(ctx, tx, sgDataKey(gid, version))
}

////////////////////////////////////////////////////////////
// SyncGroup methods between Client and Syncbase.

// TODO(hpucha): Pass blessings along.
func (sd *syncDatabase) CreateSyncGroup(ctx *context.T, call rpc.ServerCall, sgName string, spec wire.SyncGroupSpec, myInfo wire.SyncGroupMemberInfo) error {
	vlog.VI(2).Infof("sync: CreateSyncGroup: begin: %s", sgName)
	defer vlog.VI(2).Infof("sync: CreateSyncGroup: end: %s", sgName)

	ss := sd.sync.(*syncService)
	appName, dbName := sd.db.App().Name(), sd.db.Name()

	// Instantiate sg. Add self as joiner.
	gid, version := newSyncGroupId(), newSyncGroupVersion()
	sg := &interfaces.SyncGroup{
		Id:          gid,
		Name:        sgName,
		SpecVersion: version,
		Spec:        spec,
		Creator:     ss.name,
		AppName:     appName,
		DbName:      dbName,
		Status:      interfaces.SyncGroupStatusPublishPending,
		Joiners:     map[string]wire.SyncGroupMemberInfo{ss.name: myInfo},
	}

	err := store.RunInTransaction(sd.db.St(), func(tx store.Transaction) error {
		// Check permissions on Database.
		if err := sd.db.CheckPermsInternal(ctx, call, tx); err != nil {
			return err
		}

		// TODO(hpucha): Check prefix ACLs on all SG prefixes.
		// This may need another method on util.Database interface.
		// TODO(hpucha): Do some SG ACL checking. Check creator
		// has Admin privilege.

		// Reserve a log generation and position counts for the new SyncGroup.
		gen, pos := ss.reserveGenAndPosInDbLog(ctx, appName, dbName, sgOID(gid), 1)

		if err := ss.addSyncGroup(ctx, tx, version, true, "", nil, ss.id, gen, pos, sg); err != nil {
			return err
		}

		// Take a snapshot of the data to bootstrap the SyncGroup.
		return sd.bootstrapSyncGroup(ctx, tx, gid, spec.Prefixes)
	})

	if err != nil {
		return err
	}

	ss.initSyncStateInMem(ctx, appName, dbName, sgOID(gid))

	// Local SG create succeeded. Publish the SG at the chosen server, or if
	// that fails, enqueue it for later publish retries.
	if err := sd.publishSyncGroup(ctx, call, sgName); err != nil {
		ss.enqueuePublishSyncGroup(sgName, appName, dbName, true)
	}

	// Publish at the chosen mount table and in the neighborhood.
	sd.publishInMountTables(ctx, call, spec)

	return nil
}

// TODO(hpucha): Pass blessings along.
func (sd *syncDatabase) JoinSyncGroup(ctx *context.T, call rpc.ServerCall, sgName string, myInfo wire.SyncGroupMemberInfo) (wire.SyncGroupSpec, error) {
	vlog.VI(2).Infof("sync: JoinSyncGroup: begin: %s", sgName)
	defer vlog.VI(2).Infof("sync: JoinSyncGroup: end: %s", sgName)

	var sgErr error
	var sg *interfaces.SyncGroup
	nullSpec := wire.SyncGroupSpec{}

	err := store.RunInTransaction(sd.db.St(), func(tx store.Transaction) error {
		// Check permissions on Database.
		if err := sd.db.CheckPermsInternal(ctx, call, tx); err != nil {
			return err
		}

		// Check if SyncGroup already exists and get its info.
		var gid interfaces.GroupId
		gid, sgErr = getSyncGroupId(ctx, tx, sgName)
		if sgErr != nil {
			return sgErr
		}

		sg, sgErr = getSyncGroupById(ctx, tx, gid)
		if sgErr != nil {
			return sgErr
		}

		// Check SG ACL.
		if err := authorize(ctx, call.Security(), sg); err != nil {
			return err
		}

		// SyncGroup already exists, increment the number of local
		// joiners in its local state information.  This presents
		// different scenarios:
		// 1- An additional local joiner: the current number of local
		//    joiners is > 0 and the SyncGroup was already bootstrapped
		//    to the Watcher, so there is nothing else to do.
		// 2- A new local joiner after all previous local joiners had
		//    left: the number of local joiners is 0, the Watcher must
		//    be re-notified via a SyncGroup bootstrap because the last
		//    previous joiner to leave had un-notified the Watcher.  In
		//    this scenario the SyncGroup was not destroyed after the
		//    last joiner left because the SyncGroup was also published
		//    here by a remote peer and thus cannot be destroyed only
		//    based on the local joiners.
		// 3- A first local joiner for a SyncGroup that was published
		//    here from a remote Syncbase: the number of local joiners
		//    is also 0 (and the remote publish flag is set), and the
		//    Watcher must be notified via a SyncGroup bootstrap.
		// Conclusion: bootstrap if the number of local joiners is 0.
		sgState, err := getSGIdEntry(ctx, tx, gid)
		if err != nil {
			return err
		}

		if sgState.NumLocalJoiners == 0 {
			if err := sd.bootstrapSyncGroup(ctx, tx, gid, sg.Spec.Prefixes); err != nil {
				return err
			}
		}
		sgState.NumLocalJoiners++
		return setSGIdEntry(ctx, tx, gid, sgState)
	})

	// The presented blessing is allowed to make this Syncbase instance join
	// the specified SyncGroup, but this Syncbase instance has in fact
	// already joined the SyncGroup. Join is idempotent, so we simply return
	// the spec to indicate success.
	if err == nil {
		return sg.Spec, nil
	}

	// Join is not allowed (possibilities include Database permissions check
	// failed, SG ACL check failed or error during fetching SG information).
	if verror.ErrorID(sgErr) != verror.ErrNoExist.ID {
		return nullSpec, err
	}

	// Brand new join.

	// Get this Syncbase's sync module handle.
	ss := sd.sync.(*syncService)

	// Contact a SyncGroup Admin to join the SyncGroup.
	sg2, version, genvec, err := sd.joinSyncGroupAtAdmin(ctx, call, sgName, ss.name, myInfo)
	if err != nil {
		return nullSpec, err
	}

	// Verify that the app/db combination is valid for this SyncGroup.
	appName, dbName := sd.db.App().Name(), sd.db.Name()
	if sg2.AppName != appName || sg2.DbName != dbName {
		return nullSpec, verror.New(verror.ErrBadArg, ctx, "bad app/db with syncgroup")
	}

	err = store.RunInTransaction(sd.db.St(), func(tx store.Transaction) error {
		if err := ss.addSyncGroup(ctx, tx, version, false, "", genvec, 0, 0, 0, &sg2); err != nil {
			return err
		}

		// Take a snapshot of the data to bootstrap the SyncGroup.
		return sd.bootstrapSyncGroup(ctx, tx, sg2.Id, sg2.Spec.Prefixes)
	})

	if err != nil {
		return nullSpec, err
	}

	ss.initSyncStateInMem(ctx, sg2.AppName, sg2.DbName, sgOID(sg2.Id))

	// Publish at the chosen mount table and in the neighborhood.
	sd.publishInMountTables(ctx, call, sg2.Spec)

	return sg2.Spec, nil
}

func (sd *syncDatabase) GetSyncGroupNames(ctx *context.T, call rpc.ServerCall) ([]string, error) {
	vlog.VI(2).Infof("sync: GetSyncGroupNames: begin")
	defer vlog.VI(2).Infof("sync: GetSyncGroupNames: end")

	sn := sd.db.St().NewSnapshot()
	defer sn.Abort()

	// Check permissions on Database.
	if err := sd.db.CheckPermsInternal(ctx, call, sn); err != nil {
		return nil, err
	}

	// Scan all the SyncGroup names found in the Database.
	stream := sn.Scan(util.ScanPrefixArgs(sgNameKeyPrefix, ""))
	var sgNames []string
	var key []byte
	for stream.Advance() {
		sgName, err := splitSgNameKey(ctx, string(stream.Key(key)))
		if err != nil {
			return nil, err
		}
		sgNames = append(sgNames, sgName)
	}

	if err := stream.Err(); err != nil {
		return nil, err
	}

	vlog.VI(2).Infof("sync: GetSyncGroupNames: %v", sgNames)
	return sgNames, nil
}

func (sd *syncDatabase) GetSyncGroupSpec(ctx *context.T, call rpc.ServerCall, sgName string) (wire.SyncGroupSpec, string, error) {
	vlog.VI(2).Infof("sync: GetSyncGroupSpec: begin %s", sgName)
	defer vlog.VI(2).Infof("sync: GetSyncGroupSpec: end: %s", sgName)

	sn := sd.db.St().NewSnapshot()
	defer sn.Abort()

	var spec wire.SyncGroupSpec

	// Check permissions on Database.
	if err := sd.db.CheckPermsInternal(ctx, call, sn); err != nil {
		return spec, "", err
	}

	// Get the SyncGroup information.
	sg, err := getSyncGroupByName(ctx, sn, sgName)
	if err != nil {
		return spec, "", err
	}
	// TODO(hpucha): Check SyncGroup ACL.

	vlog.VI(2).Infof("sync: GetSyncGroupSpec: %s spec %v", sgName, sg.Spec)
	return sg.Spec, sg.SpecVersion, nil
}

func (sd *syncDatabase) GetSyncGroupMembers(ctx *context.T, call rpc.ServerCall, sgName string) (map[string]wire.SyncGroupMemberInfo, error) {
	vlog.VI(2).Infof("sync: GetSyncGroupMembers: begin %s", sgName)
	defer vlog.VI(2).Infof("sync: GetSyncGroupMembers: end: %s", sgName)

	sn := sd.db.St().NewSnapshot()
	defer sn.Abort()

	// Check permissions on Database.
	if err := sd.db.CheckPermsInternal(ctx, call, sn); err != nil {
		return nil, err
	}

	// Get the SyncGroup information.
	sg, err := getSyncGroupByName(ctx, sn, sgName)
	if err != nil {
		return nil, err
	}

	// TODO(hpucha): Check SyncGroup ACL.

	vlog.VI(2).Infof("sync: GetSyncGroupMembers: %s members %v", sgName, sg.Joiners)
	return sg.Joiners, nil
}

func (sd *syncDatabase) SetSyncGroupSpec(ctx *context.T, call rpc.ServerCall, sgName string, spec wire.SyncGroupSpec, version string) error {
	vlog.VI(2).Infof("sync: SetSyncGroupSpec: begin %s %v %s", sgName, spec, version)
	defer vlog.VI(2).Infof("sync: SetSyncGroupSpec: end: %s", sgName)

	if err := verifySyncGroupSpec(ctx, &spec); err != nil {
		return err
	}

	ss := sd.sync.(*syncService)
	appName, dbName := sd.db.App().Name(), sd.db.Name()

	err := store.RunInTransaction(sd.db.St(), func(tx store.Transaction) error {
		// Check permissions on Database.
		if err := sd.db.CheckPermsInternal(ctx, call, tx); err != nil {
			return err
		}

		sg, err := getSyncGroupByName(ctx, tx, sgName)
		if err != nil {
			return err
		}

		if version != NoVersion && sg.SpecVersion != version {
			return verror.NewErrBadVersion(ctx)
		}

		// Client must not modify the set of prefixes for this SyncGroup.
		if !samePrefixes(spec.Prefixes, sg.Spec.Prefixes) {
			return verror.New(verror.ErrBadArg, ctx, "cannot modify prefixes")
		}

		sgState, err := getSGIdEntry(ctx, tx, sg.Id)
		if err != nil {
			return err
		}
		if sgState.SyncPending {
			return verror.NewErrBadState(ctx)
		}

		// Reserve a log generation and position counts for the new SyncGroup.
		gen, pos := ss.reserveGenAndPosInDbLog(ctx, appName, dbName, sgOID(sg.Id), 1)

		// TODO(hpucha): Check SyncGroup ACL.

		newVersion := newSyncGroupVersion()
		sg.Spec = spec
		sg.SpecVersion = newVersion
		return ss.updateSyncGroupVersioning(ctx, tx, newVersion, true, ss.id, gen, pos, sg)
	})
	return err
}

//////////////////////////////
// Helper functions

// publishSyncGroup publishes the SyncGroup at the remote peer and update its
// status.  If the publish operation is either successful or rejected by the
// peer, the status is updated to "running" or "rejected" respectively and the
// function returns "nil" to indicate to the caller there is no need to make
// further attempts.  Otherwise an error (typically RPC error, but could also
// be a store error) is returned to the caller.
// TODO(rdaoud): make all SG admins try to publish after they join.
func (sd *syncDatabase) publishSyncGroup(ctx *context.T, call rpc.ServerCall, sgName string) error {
	st := sd.db.St()
	ss := sd.sync.(*syncService)
	appName, dbName := sd.db.App().Name(), sd.db.Name()

	gid, err := getSyncGroupId(ctx, st, sgName)
	if err != nil {
		return err
	}
	version, err := getSyncGroupVersion(ctx, st, gid)
	if err != nil {
		return err
	}
	sg, err := getSGDataEntry(ctx, st, gid, version)
	if err != nil {
		return err
	}

	if sg.Status != interfaces.SyncGroupStatusPublishPending {
		return nil
	}

	// Note: the remote peer is given the SyncGroup version and genvec at
	// the point before the post-publish update, at which time the status
	// and joiner list of the SyncGroup get updated.  This is functionally
	// correct, just not symmetrical with what happens at joiner, which
	// receives the SyncGroup state post-join.
	status := interfaces.SyncGroupStatusPublishRejected

	sgs := sgSet{gid: struct{}{}}
	gv, _, err := ss.copyDbGenInfo(ctx, appName, dbName, sgs)
	if err != nil {
		return err
	}
	// TODO(hpucha): Do we want to pick the head version corresponding to
	// the local gen of the sg? It appears that it shouldn't matter.

	c := interfaces.SyncClient(sgName)
	peer, err := c.PublishSyncGroup(ctx, ss.name, *sg, version, gv[sgOID(gid)])

	if err == nil {
		status = interfaces.SyncGroupStatusRunning
	} else {
		errId := verror.ErrorID(err)
		if errId == interfaces.ErrDupSyncGroupPublish.ID {
			// Duplicate publish: another admin already published
			// the SyncGroup, nothing else needs to happen because
			// that other admin would have updated the SyncGroup
			// status and p2p SG sync will propagate the change.
			// TODO(rdaoud): what if that other admin crashes and
			// never updates the SyncGroup status (dies permanently
			// or is ejected before the status update)?  Eventually
			// some admin must decide to update the SG status anyway
			// even if that causes extra SG mutations and conflicts.
			vlog.VI(3).Infof("sync: publishSyncGroup: %s: duplicate publish", sgName)
			return nil
		}

		if errId != verror.ErrExist.ID {
			// The publish operation failed with an error other
			// than ErrExist then it must be retried later on.
			// TODO(hpucha): Is there an RPC error that we can check here?
			vlog.VI(3).Infof("sync: publishSyncGroup: %s: failed, retry later: %v", sgName, err)
			return err
		}
	}

	// The publish operation is done because either it succeeded or it
	// failed with the ErrExist error.  Update the SyncGroup status and, if
	// the publish was successful, add the remote peer to the SyncGroup.
	vlog.VI(3).Infof("sync: publishSyncGroup: %s: peer %s: done: status %s: %v",
		sgName, peer, status.String(), err)

	err = store.RunInTransaction(st, func(tx store.Transaction) error {
		// Ensure SG still exists.
		sg, err := getSyncGroupById(ctx, tx, gid)
		if err != nil {
			return err
		}

		// Reserve a log generation and position counts for the new
		// SyncGroup version.
		gen, pos := ss.reserveGenAndPosInDbLog(ctx, appName, dbName, sgOID(gid), 1)

		sg.Status = status
		if status == interfaces.SyncGroupStatusRunning {
			// TODO(hpucha): Default priority?
			sg.Joiners[peer] = wire.SyncGroupMemberInfo{}
		}

		return ss.updateSyncGroupVersioning(ctx, tx, NoVersion, true, ss.id, gen, pos, sg)
	})
	if err != nil {
		vlog.Errorf("sync: publishSyncGroup: cannot update SyncGroup %s status to %s: %v",
			sgName, status.String(), err)
	}
	return err
}

// bootstrapSyncGroup inserts into the transaction log a SyncGroup operation and
// a set of Snapshot operations to notify the sync watcher about the SyncGroup
// prefixes to start accepting and the initial state of existing store keys that
// match these prefixes (both data and permission keys).
// TODO(rdaoud): this operation scans the managed keys of the database and can
// be time consuming.  Consider doing it asynchronously and letting the server
// reply to the client earlier.  However it must happen within the scope of this
// transaction (and its snapshot view).
func (sd *syncDatabase) bootstrapSyncGroup(ctx *context.T, tx store.Transaction, sgId interfaces.GroupId, prefixes []wire.SyncGroupPrefix) error {
	if len(prefixes) == 0 {
		return verror.New(verror.ErrInternal, ctx, "no prefixes specified")
	}

	// Get the store options to retrieve the list of managed key prefixes.
	opts, err := watchable.GetOptions(sd.db.St())
	if err != nil {
		return err
	}
	if len(opts.ManagedPrefixes) == 0 {
		return verror.New(verror.ErrInternal, ctx, "store has no managed prefixes")
	}

	prefixStrs := make([]string, len(prefixes))
	for i, p := range prefixes {
		prefixStrs[i] = toTableRowPrefixStr(p)
	}
	// Notify the watcher of the SyncGroup prefixes to start accepting.
	if err := watchable.AddSyncGroupOp(ctx, tx, sgId, prefixStrs, false); err != nil {
		return err
	}

	// Loop over the store managed key prefixes (e.g. data and permissions).
	// For each one, scan the ranges of the given SyncGroup prefixes.  For
	// each matching key, insert a snapshot operation in the log.  Scanning
	// is done over the version entries to retrieve the matching keys and
	// their version numbers (the key values).  Remove the version prefix
	// from the key used in the snapshot operation.
	for _, mp := range opts.ManagedPrefixes {
		for _, p := range prefixStrs {
			start, limit := util.ScanPrefixArgs(util.JoinKeyParts(util.VersionPrefix, mp), p)
			stream := tx.Scan(start, limit)
			for stream.Advance() {
				k, v := stream.Key(nil), stream.Value(nil)
				parts := util.SplitKeyParts(string(k))
				if len(parts) < 2 {
					vlog.Fatalf("sync: bootstrapSyncGroup: invalid version key %s", string(k))

				}
				key := []byte(util.JoinKeyParts(parts[1:]...))
				if err := watchable.AddSyncSnapshotOp(ctx, tx, key, v); err != nil {
					return err
				}

			}
			if err := stream.Err(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (sd *syncDatabase) publishInMountTables(ctx *context.T, call rpc.ServerCall, spec wire.SyncGroupSpec) error {
	// Get this Syncbase's sync module handle.
	ss := sd.sync.(*syncService)
	ss.nameLock.Lock()
	defer ss.nameLock.Unlock()

	for _, mt := range spec.MountTables {
		name := naming.Join(mt, ss.name)
		// AddName is idempotent.
		if err := call.Server().AddName(name); err != nil {
			return err
		}
	}

	// TODO(hpucha): Do we have to publish in neighborhood explicitly?

	return nil
}

func (sd *syncDatabase) joinSyncGroupAtAdmin(ctx *context.T, call rpc.ServerCall, sgName, name string, myInfo wire.SyncGroupMemberInfo) (interfaces.SyncGroup, string, interfaces.PrefixGenVector, error) {
	c := interfaces.SyncClient(sgName)
	return c.JoinSyncGroupAtAdmin(ctx, sgName, name, myInfo)

	// TODO(hpucha): Try to join using an Admin on neighborhood if the publisher is not reachable.
}

func authorize(ctx *context.T, call security.Call, sg *interfaces.SyncGroup) error {
	auth := access.TypicalTagTypePermissionsAuthorizer(sg.Spec.Perms)
	if err := auth.Authorize(ctx, call); err != nil {
		return verror.New(verror.ErrNoAccess, ctx, err)
	}
	return nil
}

////////////////////////////////////////////////////////////
// Methods for SyncGroup create/join between Syncbases.

func (s *syncService) PublishSyncGroup(ctx *context.T, call rpc.ServerCall, publisher string, sg interfaces.SyncGroup, version string, genvec interfaces.PrefixGenVector) (string, error) {
	st, err := s.getDbStore(ctx, call, sg.AppName, sg.DbName)
	if err != nil {
		return s.name, err
	}

	err = store.RunInTransaction(st, func(tx store.Transaction) error {
		gid, err := getSyncGroupId(ctx, tx, sg.Name)
		if err != nil && verror.ErrorID(err) != verror.ErrNoExist.ID {
			return err
		}

		if err == nil {
			// SG name already claimed.  Note that in this case of
			// split-brain (same SG name, different IDs), those in
			// SG ID being rejected here do not benefit from the
			// de-duping optimization below and will end up making
			// duplicate SG mutations to set the status, yielding
			// more SG conflicts.  It is functionally correct but
			// bypasses the de-dup optimization for the rejected SG.
			if gid != sg.Id {
				return verror.New(verror.ErrExist, ctx, sg.Name)
			}

			// SG exists locally, either locally created/joined or
			// previously published.  Make it idempotent for the
			// same publisher, otherwise it's a duplicate.
			state, err := getSGIdEntry(ctx, tx, gid)
			if err != nil {
				return err
			}
			if state.RemotePublisher == "" {
				// Locally created/joined SyncGroup: update its
				// state to include the publisher.
				state.RemotePublisher = publisher
				return setSGIdEntry(ctx, tx, gid, state)
			}
			if publisher == state.RemotePublisher {
				// Same previous publisher: nothing to change,
				// the old genvec and version info is valid.
				return nil
			}
			return interfaces.NewErrDupSyncGroupPublish(ctx, sg.Name)
		}

		// Publish the SyncGroup.

		// TODO(hpucha): Use some ACL check to allow/deny publishing.
		// TODO(hpucha): Ensure node is on Admin ACL.

		return s.addSyncGroup(ctx, tx, version, false, publisher, genvec, 0, 0, 0, &sg)
	})

	if err == nil {
		s.initSyncStateInMem(ctx, sg.AppName, sg.DbName, sgOID(sg.Id))
	}
	return s.name, err
}

func (s *syncService) JoinSyncGroupAtAdmin(ctx *context.T, call rpc.ServerCall, sgName, joinerName string, joinerInfo wire.SyncGroupMemberInfo) (interfaces.SyncGroup, string, interfaces.PrefixGenVector, error) {
	vlog.VI(2).Infof("sync: JoinSyncGroupAtAdmin: begin: %s from peer %s", sgName, joinerName)
	defer vlog.VI(2).Infof("sync: JoinSyncGroupAtAdmin: end: %s from peer %s", sgName, joinerName)

	var dbSt store.Store
	var gid interfaces.GroupId
	var err error
	var stAppName, stDbName string
	nullSG, nullGV := interfaces.SyncGroup{}, interfaces.PrefixGenVector{}

	// Find the database store for this SyncGroup.
	//
	// TODO(hpucha): At a high level, we have yet to decide if the SG name
	// is stand-alone or is derived from the app/db namespace, based on the
	// feedback from app developers (see discussion in SyncGroup API
	// doc). If we decide to keep the SG name as stand-alone, this scan can
	// be optimized by a lazy cache of sgname to <app, db> info.
	s.forEachDatabaseStore(ctx, func(appName, dbName string, st store.Store) bool {
		if gid, err = getSyncGroupId(ctx, st, sgName); err == nil {
			// Found the SyncGroup being looked for.
			dbSt = st
			stAppName, stDbName = appName, dbName
			return true
		}
		return false
	})

	// SyncGroup not found.
	if err != nil {
		return nullSG, "", nullGV, verror.New(verror.ErrNoExist, ctx, "SyncGroup not found", sgName)
	}

	version := newSyncGroupVersion()
	var sg *interfaces.SyncGroup
	var gen, pos uint64

	err = store.RunInTransaction(dbSt, func(tx store.Transaction) error {
		var err error
		sg, err = getSyncGroupById(ctx, tx, gid)
		if err != nil {
			return err
		}

		// Check SG ACL.
		if err := authorize(ctx, call.Security(), sg); err != nil {
			return err
		}

		// Check that the SG is not in pending state.
		state, err := getSGIdEntry(ctx, tx, gid)
		if err != nil {
			return err
		}
		if state.SyncPending {
			return verror.NewErrBadState(ctx)
		}

		// Reserve a log generation and position counts for the new SyncGroup.
		gen, pos = s.reserveGenAndPosInDbLog(ctx, stAppName, stDbName, sgOID(gid), 1)

		// Add to joiner list.
		sg.Joiners[joinerName] = joinerInfo
		return s.updateSyncGroupVersioning(ctx, tx, version, true, s.id, gen, pos, sg)
	})

	if err != nil {
		return nullSG, "", nullGV, err
	}

	sgs := sgSet{gid: struct{}{}}
	gv, _, err := s.copyDbGenInfo(ctx, stAppName, stDbName, sgs)
	if err != nil {
		return nullSG, "", nullGV, err
	}
	// The retrieved genvector does not contain the mutation that adds the
	// joiner to the list since initiator is the one checkpointing the
	// generations. Add that generation to this genvector.
	gv[sgOID(gid)][s.id] = gen

	vlog.VI(2).Infof("sync: JoinSyncGroupAtAdmin: returning: sg %v, vers %v, genvec %v", sg, version, gv[sgOID(gid)])
	return *sg, version, gv[sgOID(gid)], nil
}
