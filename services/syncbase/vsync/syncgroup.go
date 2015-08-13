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
	"strings"
	"time"

	wire "v.io/syncbase/v23/services/syncbase/nosql"
	"v.io/syncbase/x/ref/services/syncbase/server/interfaces"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/server/watchable"
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/security/access"
	"v.io/v23/verror"
	"v.io/v23/vom"
	"v.io/x/lib/vlog"
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
// to within each App/Database (i.e. global database name).  It's a mapping of
// global DB names to sets of SyncGroup member information.
type memberInfo struct {
	db2sg map[string]sgMemberInfo
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
	if len(sg.Spec.Prefixes) == 0 {
		return verror.New(verror.ErrBadArg, ctx, "group has no prefixes specified")
	}
	return nil
}

// addSyncGroup adds a new SyncGroup given its information.
func addSyncGroup(ctx *context.T, tx store.Transaction, sg *interfaces.SyncGroup) error {
	// Verify SyncGroup before storing it since it may have been received
	// from a remote peer.
	if err := verifySyncGroup(ctx, sg); err != nil {
		return err
	}

	if ok, err := hasSGDataEntry(tx, sg.Id); err != nil {
		return err
	} else if ok {
		return verror.New(verror.ErrExist, ctx, "group id already exists")
	}
	if ok, err := hasSGNameEntry(tx, sg.Name); err != nil {
		return err
	} else if ok {
		return verror.New(verror.ErrExist, ctx, "group name already exists")
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
func getSyncGroupId(ctx *context.T, st store.StoreReader, name string) (interfaces.GroupId, error) {
	return getSGNameEntry(ctx, st, name)
}

// getSyncGroupName retrieves the SyncGroup name given its ID.
func getSyncGroupName(ctx *context.T, st store.StoreReader, gid interfaces.GroupId) (string, error) {
	sg, err := getSyncGroupById(ctx, st, gid)
	if err != nil {
		return "", err
	}
	return sg.Name, nil
}

// getSyncGroupById retrieves the SyncGroup given its ID.
func getSyncGroupById(ctx *context.T, st store.StoreReader, gid interfaces.GroupId) (*interfaces.SyncGroup, error) {
	return getSGDataEntry(ctx, st, gid)
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
	if err = delSGNameEntry(ctx, tx, sg.Name); err != nil {
		return err
	}
	return delSGDataEntry(ctx, tx, sg.Id)
}

// delSyncGroupByName deletes the SyncGroup given its name.
func delSyncGroupByName(ctx *context.T, tx store.Transaction, name string) error {
	gid, err := getSyncGroupId(ctx, tx, name)
	if err != nil {
		return err
	}
	return delSyncGroupById(ctx, tx, gid)
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
			for member, info := range sg.Joiners {
				if _, ok := newMembers[member]; !ok {
					newMembers[member] = &memberInfo{db2sg: make(map[string]sgMemberInfo)}
				}
				if _, ok := newMembers[member].db2sg[name]; !ok {
					newMembers[member].db2sg[name] = make(sgMemberInfo)
				}
				newMembers[member].db2sg[name][sg.Id] = info
			}
			return false
		})
		return false
	})

	view.members = newMembers
	view.expiration = time.Now().Add(memberViewTTL)
}

// forEachSyncGroup iterates over all SyncGroups in the Database and invokes
// the callback function on each one.  The callback returns a "done" flag to
// make forEachSyncGroup() stop the iteration earlier; otherwise the function
// loops across all SyncGroups in the Database.
func forEachSyncGroup(st store.StoreReader, callback func(*interfaces.SyncGroup) bool) {
	scanStart, scanLimit := util.ScanPrefixArgs(sgDataKeyScanPrefix, "")
	stream := st.Scan(scanStart, scanLimit)
	for stream.Advance() {
		var sg interfaces.SyncGroup
		if vom.Decode(stream.Value(nil), &sg) != nil {
			vlog.Errorf("sync: forEachSyncGroup: invalid SyncGroup value for key %s", string(stream.Key(nil)))
			continue
		}

		if callback(&sg) {
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
	infoCopy := &memberInfo{make(map[string]sgMemberInfo)}
	for gdbName, sgInfo := range info.db2sg {
		infoCopy.db2sg[gdbName] = make(sgMemberInfo)
		for gid, mi := range sgInfo {
			infoCopy.db2sg[gdbName][gid] = mi
		}
	}

	return infoCopy
}

// Low-level utility functions to access DB entries without tracking their
// relationships.
// Use the functions above to manipulate SyncGroups.

var (
	// sgDataKeyScanPrefix is the prefix used to scan SyncGroup data entries.
	sgDataKeyScanPrefix = util.JoinKeyParts(util.SyncPrefix, sgPrefix, "d")

	// sgNameKeyScanPrefix is the prefix used to scan SyncGroup name entries.
	sgNameKeyScanPrefix = util.JoinKeyParts(util.SyncPrefix, sgPrefix, "n")
)

// sgDataKey returns the key used to access the SyncGroup data entry.
func sgDataKey(gid interfaces.GroupId) string {
	return util.JoinKeyParts(util.SyncPrefix, sgPrefix, "d", fmt.Sprintf("%d", gid))
}

// sgNameKey returns the key used to access the SyncGroup name entry.
func sgNameKey(name string) string {
	return util.JoinKeyParts(util.SyncPrefix, sgPrefix, "n", name)
}

// splitSgNameKey is the inverse of sgNameKey and returns the SyncGroup name.
func splitSgNameKey(ctx *context.T, key string) (string, error) {
	prefix := util.JoinKeyParts(util.SyncPrefix, sgPrefix, "n", "")

	// Note that the actual SyncGroup name may contain ":" as a separator.
	if !strings.HasPrefix(key, prefix) {
		return "", verror.New(verror.ErrInternal, ctx, "invalid sgNamekey", key)
	}
	return strings.TrimPrefix(key, prefix), nil
}

// hasSGDataEntry returns true if the SyncGroup data entry exists.
func hasSGDataEntry(sntx store.SnapshotOrTransaction, gid interfaces.GroupId) (bool, error) {
	// TODO(rdaoud): optimize to avoid the unneeded fetch/decode of the data.
	var sg interfaces.SyncGroup
	if err := util.Get(nil, sntx, sgDataKey(gid), &sg); err != nil {
		if verror.ErrorID(err) == verror.ErrNoExist.ID {
			err = nil
		}
		return false, err
	}
	return true, nil
}

// hasSGNameEntry returns true if the SyncGroup name entry exists.
func hasSGNameEntry(sntx store.SnapshotOrTransaction, name string) (bool, error) {
	// TODO(rdaoud): optimize to avoid the unneeded fetch/decode of the data.
	var gid interfaces.GroupId
	if err := util.Get(nil, sntx, sgNameKey(name), &gid); err != nil {
		if verror.ErrorID(err) == verror.ErrNoExist.ID {
			err = nil
		}
		return false, err
	}
	return true, nil
}

// setSGDataEntry stores the SyncGroup data entry.
func setSGDataEntry(ctx *context.T, tx store.Transaction, gid interfaces.GroupId, sg *interfaces.SyncGroup) error {
	return util.Put(ctx, tx, sgDataKey(gid), sg)
}

// setSGNameEntry stores the SyncGroup name entry.
func setSGNameEntry(ctx *context.T, tx store.Transaction, name string, gid interfaces.GroupId) error {
	return util.Put(ctx, tx, sgNameKey(name), gid)
}

// getSGDataEntry retrieves the SyncGroup data for a given group ID.
func getSGDataEntry(ctx *context.T, st store.StoreReader, gid interfaces.GroupId) (*interfaces.SyncGroup, error) {
	var sg interfaces.SyncGroup
	if err := util.Get(ctx, st, sgDataKey(gid), &sg); err != nil {
		return nil, err
	}
	return &sg, nil
}

// getSGNameEntry retrieves the SyncGroup name to ID mapping.
func getSGNameEntry(ctx *context.T, st store.StoreReader, name string) (interfaces.GroupId, error) {
	var gid interfaces.GroupId
	if err := util.Get(ctx, st, sgNameKey(name), &gid); err != nil {
		return gid, err
	}
	return gid, nil
}

// delSGDataEntry deletes the SyncGroup data entry.
func delSGDataEntry(ctx *context.T, tx store.Transaction, gid interfaces.GroupId) error {
	return util.Delete(ctx, tx, sgDataKey(gid))
}

// delSGNameEntry deletes the SyncGroup name to ID mapping.
func delSGNameEntry(ctx *context.T, tx store.Transaction, name string) error {
	return util.Delete(ctx, tx, sgNameKey(name))
}

////////////////////////////////////////////////////////////
// SyncGroup methods between Client and Syncbase.

// TODO(hpucha): Pass blessings along.
func (sd *syncDatabase) CreateSyncGroup(ctx *context.T, call rpc.ServerCall, sgName string, spec wire.SyncGroupSpec, myInfo wire.SyncGroupMemberInfo) error {
	vlog.VI(2).Infof("sync: CreateSyncGroup: begin: %s", sgName)
	defer vlog.VI(2).Infof("sync: CreateSyncGroup: end: %s", sgName)

	err := store.RunInTransaction(sd.db.St(), func(tx store.Transaction) error {
		// Check permissions on Database.
		if err := sd.db.CheckPermsInternal(ctx, call, tx); err != nil {
			return err
		}

		// TODO(hpucha): Check prefix ACLs on all SG prefixes.
		// This may need another method on util.Database interface.

		// TODO(hpucha): Do some SG ACL checking. Check creator
		// has Admin privilege.

		// Get this Syncbase's sync module handle.
		ss := sd.sync.(*syncService)

		// Instantiate sg. Add self as joiner.
		sg := &interfaces.SyncGroup{
			Id:          newSyncGroupId(),
			Name:        sgName,
			SpecVersion: newSyncGroupVersion(),
			Spec:        spec,
			Creator:     ss.name,
			AppName:     sd.db.App().Name(),
			DbName:      sd.db.Name(),
			Status:      interfaces.SyncGroupStatusPublishPending,
			Joiners:     map[string]wire.SyncGroupMemberInfo{ss.name: myInfo},
		}

		if err := addSyncGroup(ctx, tx, sg); err != nil {
			return err
		}

		// TODO(hpucha): Bootstrap DAG/Genvector etc for syncing the SG metadata.

		// Take a snapshot of the data to bootstrap the SyncGroup.
		return sd.bootstrapSyncGroup(ctx, tx, spec.Prefixes)
	})

	if err != nil {
		return err
	}

	// Local SG create succeeded. Publish the SG at the chosen server.
	sd.publishSyncGroup(ctx, call, sgName)

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

		// Check if SyncGroup already exists.
		sg, sgErr = getSyncGroupByName(ctx, tx, sgName)
		if sgErr != nil {
			return sgErr
		}

		// SyncGroup already exists. Possibilities include created
		// locally, already joined locally or published at the device as
		// a result of SyncGroup creation on a different device.
		//
		// TODO(hpucha): Handle the above cases. If the SG was published
		// locally, but not joined, we need to bootstrap the DAG and
		// watcher. If multiple joins are done locally, we may want to
		// ref count the SG state and track the leaves accordingly. So
		// we may need to add some local state for each SyncGroup.

		// Check SG ACL.
		return authorize(ctx, call.Security(), sg)
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
	sg = &interfaces.SyncGroup{}
	*sg, err = sd.joinSyncGroupAtAdmin(ctx, call, sgName, ss.name, myInfo)
	if err != nil {
		return nullSpec, err
	}

	// Verify that the app/db combination is valid for this SyncGroup.
	if sg.AppName != sd.db.App().Name() || sg.DbName != sd.db.Name() {
		return nullSpec, verror.New(verror.ErrBadArg, ctx, "bad app/db with syncgroup")
	}

	err = store.RunInTransaction(sd.db.St(), func(tx store.Transaction) error {

		// TODO(hpucha): Bootstrap DAG/Genvector etc for syncing the SG metadata.

		// TODO(hpucha): Get SG Deltas from Admin device.

		if err := addSyncGroup(ctx, tx, sg); err != nil {
			return err
		}

		// Take a snapshot of the data to bootstrap the SyncGroup.
		return sd.bootstrapSyncGroup(ctx, tx, sg.Spec.Prefixes)
	})

	if err != nil {
		return nullSpec, err
	}

	// Publish at the chosen mount table and in the neighborhood.
	sd.publishInMountTables(ctx, call, sg.Spec)

	return sg.Spec, nil
}

func (sd *syncDatabase) GetSyncGroupNames(ctx *context.T, call rpc.ServerCall) ([]string, error) {
	var sgNames []string

	vlog.VI(2).Infof("sync: GetSyncGroupNames: begin")
	defer vlog.VI(2).Infof("sync: GetSyncGroupNames: end: %v", sgNames)

	sn := sd.db.St().NewSnapshot()
	defer sn.Abort()

	// Check permissions on Database.
	if err := sd.db.CheckPermsInternal(ctx, call, sn); err != nil {
		return nil, err
	}

	// Scan all the SyncGroup names found in the Database.
	scanStart, scanLimit := util.ScanPrefixArgs(sgNameKeyScanPrefix, "")
	stream := sn.Scan(scanStart, scanLimit)
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

	return sgNames, nil
}

func (sd *syncDatabase) GetSyncGroupSpec(ctx *context.T, call rpc.ServerCall, sgName string) (wire.SyncGroupSpec, string, error) {
	var spec wire.SyncGroupSpec

	vlog.VI(2).Infof("sync: GetSyncGroupSpec: begin %s", sgName)
	defer vlog.VI(2).Infof("sync: GetSyncGroupSpec: end: %s spec %v", sgName, spec)

	sn := sd.db.St().NewSnapshot()
	defer sn.Abort()

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

	spec = sg.Spec
	return spec, sg.SpecVersion, nil
}

func (sd *syncDatabase) GetSyncGroupMembers(ctx *context.T, call rpc.ServerCall, sgName string) (map[string]wire.SyncGroupMemberInfo, error) {
	var members map[string]wire.SyncGroupMemberInfo

	vlog.VI(2).Infof("sync: GetSyncGroupMembers: begin %s", sgName)
	defer vlog.VI(2).Infof("sync: GetSyncGroupMembers: end: %s members %v", sgName, members)

	sn := sd.db.St().NewSnapshot()
	defer sn.Abort()

	// Check permissions on Database.
	if err := sd.db.CheckPermsInternal(ctx, call, sn); err != nil {
		return members, err
	}

	// Get the SyncGroup information.
	sg, err := getSyncGroupByName(ctx, sn, sgName)
	if err != nil {
		return members, err
	}

	// TODO(hpucha): Check SyncGroup ACL.

	members = sg.Joiners
	return members, nil
}

// TODO(hpucha): Enable syncing syncgroup metadata.
func (sd *syncDatabase) SetSyncGroupSpec(ctx *context.T, call rpc.ServerCall, sgName string, spec wire.SyncGroupSpec, version string) error {
	vlog.VI(2).Infof("sync: SetSyncGroupSpec: begin %s %v %s", sgName, spec, version)
	defer vlog.VI(2).Infof("sync: SetSyncGroupSpec: end: %s", sgName)

	err := store.RunInTransaction(sd.db.St(), func(tx store.Transaction) error {
		// Check permissions on Database.
		if err := sd.db.CheckPermsInternal(ctx, call, tx); err != nil {
			return err
		}

		sg, err := getSyncGroupByName(ctx, tx, sgName)
		if err != nil {
			return err
		}

		// TODO(hpucha): Check SyncGroup ACL. Perform version checking.

		sg.Spec = spec
		return setSGDataEntry(ctx, tx, sg.Id, sg)
	})
	return err
}

//////////////////////////////
// Helper functions

// TODO(hpucha): Call this periodically until we are able to contact the remote peer.
func (sd *syncDatabase) publishSyncGroup(ctx *context.T, call rpc.ServerCall, sgName string) error {
	sg, err := getSyncGroupByName(ctx, sd.db.St(), sgName)
	if err != nil {
		return err
	}

	if sg.Status != interfaces.SyncGroupStatusPublishPending {
		return nil
	}

	c := interfaces.SyncClient(sgName)
	err = c.PublishSyncGroup(ctx, *sg)

	// Publish failed temporarily. Retry later.
	// TODO(hpucha): Is there an RPC error that we can check here?
	if err != nil && verror.ErrorID(err) != verror.ErrExist.ID {
		return err
	}

	// Publish succeeded.
	if err == nil {
		// TODO(hpucha): Get SG Deltas from publisher. Obtaining the
		// new version from the publisher prevents SG conflicts.
		return err
	}

	// Publish rejected. Persist that to avoid retrying in the
	// future and to remember the split universe scenario.
	err = store.RunInTransaction(sd.db.St(), func(tx store.Transaction) error {
		// Ensure SG still exists.
		sg, err := getSyncGroupByName(ctx, tx, sgName)
		if err != nil {
			return err
		}

		sg.Status = interfaces.SyncGroupStatusPublishRejected
		return setSGDataEntry(ctx, tx, sg.Id, sg)
	})
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
func (sd *syncDatabase) bootstrapSyncGroup(ctx *context.T, tx store.Transaction, prefixes []string) error {
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

	// Notify the watcher of the SyncGroup prefixes to start accepting.
	if err := watchable.AddSyncGroupOp(ctx, tx, prefixes, false); err != nil {
		return err
	}

	// Loop over the store managed key prefixes (e.g. data and permissions).
	// For each one, scan the ranges of the given SyncGroup prefixes.  For
	// each matching key, insert a snapshot operation in the log.  Scanning
	// is done over the version entries to retrieve the matching keys and
	// their version numbers (the key values).  Remove the version prefix
	// from the key used in the snapshot operation.
	// TODO(rdaoud): for SyncGroup prefixes, there should be a separation
	// between their representation at the client (a list of (db, prefix)
	// tuples) and internally as strings that match the store's key format.
	for _, mp := range opts.ManagedPrefixes {
		for _, p := range prefixes {
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

	for _, mt := range spec.MountTables {
		name := naming.Join(mt, ss.name)
		// TODO(hpucha): Is this add idempotent? Appears to be from code.
		// Confirm that it is ok to use absolute names here.
		if err := ss.server.AddName(name); err != nil {
			return err
		}
	}

	// TODO(hpucha): Do we have to publish in neighborhood explicitly?

	return nil
}

func (sd *syncDatabase) joinSyncGroupAtAdmin(ctx *context.T, call rpc.ServerCall, sgName, name string, myInfo wire.SyncGroupMemberInfo) (interfaces.SyncGroup, error) {
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

func (s *syncService) PublishSyncGroup(ctx *context.T, call rpc.ServerCall, sg interfaces.SyncGroup) error {
	st, err := s.getDbStore(ctx, call, sg.AppName, sg.DbName)
	if err != nil {
		return err
	}

	err = store.RunInTransaction(st, func(tx store.Transaction) error {
		localSG, err := getSyncGroupByName(ctx, tx, sg.Name)

		if err != nil && verror.ErrorID(err) != verror.ErrNoExist.ID {
			return err
		}

		// SG name already claimed.
		if err == nil && localSG.Id != sg.Id {
			return verror.New(verror.ErrExist, ctx, sg.Name)
		}

		// TODO(hpucha): Bootstrap DAG/Genvector etc for syncing the SG
		// metadata if needed.
		//
		// TODO(hpucha): Catch up on SG versions so far.

		// SG already published. Update if needed.
		if err == nil && localSG.Id == sg.Id {
			if localSG.Status == interfaces.SyncGroupStatusPublishPending {
				localSG.Status = interfaces.SyncGroupStatusRunning
				return setSGDataEntry(ctx, tx, localSG.Id, localSG)
			}
			return nil
		}

		// Publish the SyncGroup.

		// TODO(hpucha): Use some ACL check to allow/deny publishing.
		// TODO(hpucha): Ensure node is on Admin ACL.

		// TODO(hpucha): Default priority?
		sg.Joiners[s.name] = wire.SyncGroupMemberInfo{}
		sg.Status = interfaces.SyncGroupStatusRunning
		return addSyncGroup(ctx, tx, &sg)
	})

	return err
}

func (s *syncService) JoinSyncGroupAtAdmin(ctx *context.T, call rpc.ServerCall, sgName, joinerName string, joinerInfo wire.SyncGroupMemberInfo) (interfaces.SyncGroup, error) {
	var dbSt store.Store
	var gid interfaces.GroupId
	var err error

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
			return true
		}
		return false
	})

	// SyncGroup not found.
	if err != nil {
		return interfaces.SyncGroup{}, verror.New(verror.ErrNoExist, ctx, "SyncGroup not found", sgName)
	}

	var sg *interfaces.SyncGroup
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

		// Add to joiner list.
		sg.Joiners[joinerName] = joinerInfo
		return setSGDataEntry(ctx, tx, sg.Id, sg)
	})

	if err != nil {
		return interfaces.SyncGroup{}, err
	}
	return *sg, nil
}
