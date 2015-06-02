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
	"strings"
	"time"

	"v.io/syncbase/x/ref/services/syncbase/server/interfaces"
	"v.io/syncbase/x/ref/services/syncbase/server/util"
	"v.io/syncbase/x/ref/services/syncbase/store"

	wire "v.io/syncbase/v23/services/syncbase/nosql"

	"v.io/v23/context"
	"v.io/v23/rpc"
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
// to.
type memberInfo struct {
	gid2info map[interfaces.GroupId]wire.SyncGroupMemberInfo
}

// newSyncGroupVersion generates a random SyncGroup version ("etag").
func newSyncGroupVersion() string {
	return fmt.Sprintf("%x", rng.Int63())
}

// newSyncGroupId generates a random SyncGroup ID.
func newSyncGroupId() interfaces.GroupId {
	return interfaces.GroupId(rng.Int63())
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
func addSyncGroup(ctx *context.T, tx store.StoreReadWriter, sg *interfaces.SyncGroup) error {
	_ = tx.(store.Transaction)

	// Verify SyncGroup before storing it since it may have been received
	// from a remote peer.
	if err := verifySyncGroup(ctx, sg); err != nil {
		return err
	}

	if hasSGDataEntry(tx, sg.Id) {
		return verror.New(verror.ErrExist, ctx, "group id already exists")
	}
	if hasSGNameEntry(tx, sg.Name) {
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
func delSyncGroupById(ctx *context.T, tx store.StoreReadWriter, gid interfaces.GroupId) error {
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

// refreshMembersIfExpired updates the aggregate view of SyncGroup members
// across databases if the view has expired.
// TODO(rdaoud): track dirty apps/dbs since the last refresh and incrementally
// update the membership view for them instead of always scanning all of them.
func (s *syncService) refreshMembersIfExpired(ctx *context.T) {
	view := s.allMembers
	if view == nil {
		// The empty expiration time in Go is before "now" and treated as expired
		// below.
		view = &memberView{expiration: time.Time{}, members: make(map[string]*memberInfo)}
		s.allMembers = view
	}

	if time.Now().Before(view.expiration) {
		return
	}

	// Create a new aggregate view of SyncGroup members across all app databases.
	newMembers := make(map[string]*memberInfo)
	scanStart, scanLimit := util.ScanPrefixArgs(sgDataKeyScanPrefix(), "")

	s.forEachDatabaseStore(ctx, func(st store.Store) bool {
		// For each database, fetch its SyncGroup data entries by scanning their
		// prefix range.  Use a database snapshot for the scan.
		sn := st.NewSnapshot()
		defer sn.Close()

		stream := sn.Scan(scanStart, scanLimit)
		for stream.Advance() {
			var sg interfaces.SyncGroup
			if vom.Decode(stream.Value(nil), &sg) != nil {
				vlog.Errorf("invalid SyncGroup value for key %s", string(stream.Key(nil)))
				continue
			}

			// Add all members of this SyncGroup to the membership view.
			// A member's info is different across SyncGroups, so gather all of them.
			for member, info := range sg.Joiners {
				if _, ok := newMembers[member]; !ok {
					newMembers[member] = &memberInfo{
						gid2info: make(map[interfaces.GroupId]wire.SyncGroupMemberInfo),
					}
				}
				newMembers[member].gid2info[sg.Id] = info
			}
		}
		return false
	})

	view.members = newMembers
	view.expiration = time.Now().Add(memberViewTTL)
}

// getMembers returns all SyncGroup members and the count of SyncGroups each one
// joined.
func (s *syncService) getMembers(ctx *context.T) map[string]uint32 {
	s.refreshMembersIfExpired(ctx)

	members := make(map[string]uint32)
	for member, info := range s.allMembers.members {
		members[member] = uint32(len(info.gid2info))
	}

	return members
}

// Low-level utility functions to access DB entries without tracking their
// relationships.
// Use the functions above to manipulate SyncGroups.

// sgDataKeyScanPrefix returns the prefix used to scan SyncGroup data entries.
func sgDataKeyScanPrefix() string {
	return util.JoinKeyParts(util.SyncPrefix, "sg", "d")
}

// sgDataKey returns the key used to access the SyncGroup data entry.
func sgDataKey(gid interfaces.GroupId) string {
	return util.JoinKeyParts(util.SyncPrefix, "sg", "d", fmt.Sprintf("%d", gid))
}

// sgNameKey returns the key used to access the SyncGroup name entry.
func sgNameKey(name string) string {
	return util.JoinKeyParts(util.SyncPrefix, "sg", "n", name)
}

// hasSGDataEntry returns true if the SyncGroup data entry exists.
func hasSGDataEntry(st store.StoreReader, gid interfaces.GroupId) bool {
	// TODO(rdaoud): optimize to avoid the unneeded fetch/decode of the data.
	var sg interfaces.SyncGroup
	if err := util.GetObject(st, sgDataKey(gid), &sg); err != nil {
		return false
	}
	return true
}

// hasSGNameEntry returns true if the SyncGroup name entry exists.
func hasSGNameEntry(st store.StoreReader, name string) bool {
	// TODO(rdaoud): optimize to avoid the unneeded fetch/decode of the data.
	var gid interfaces.GroupId
	if err := util.GetObject(st, sgNameKey(name), &gid); err != nil {
		return false
	}
	return true
}

// setSGDataEntry stores the SyncGroup data entry.
func setSGDataEntry(ctx *context.T, tx store.StoreReadWriter, gid interfaces.GroupId, sg *interfaces.SyncGroup) error {
	_ = tx.(store.Transaction)

	if err := util.PutObject(tx, sgDataKey(gid), sg); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// setSGNameEntry stores the SyncGroup name entry.
func setSGNameEntry(ctx *context.T, tx store.StoreReadWriter, name string, gid interfaces.GroupId) error {
	_ = tx.(store.Transaction)

	if err := util.PutObject(tx, sgNameKey(name), gid); err != nil {
		return verror.New(verror.ErrInternal, ctx, err)
	}
	return nil
}

// getSGDataEntry retrieves the SyncGroup data for a given group ID.
func getSGDataEntry(ctx *context.T, st store.StoreReader, gid interfaces.GroupId) (*interfaces.SyncGroup, error) {
	var sg interfaces.SyncGroup
	if err := util.GetObject(st, sgDataKey(gid), &sg); err != nil {
		return nil, verror.New(verror.ErrInternal, ctx, err)
	}
	return &sg, nil
}

// getSGNameEntry retrieves the SyncGroup name to ID mapping.
func getSGNameEntry(ctx *context.T, st store.StoreReader, name string) (interfaces.GroupId, error) {
	var gid interfaces.GroupId
	if err := util.GetObject(st, sgNameKey(name), &gid); err != nil {
		return gid, verror.New(verror.ErrInternal, ctx, err)
	}
	return gid, nil
}

// delSGDataEntry deletes the SyncGroup data entry.
func delSGDataEntry(ctx *context.T, tx store.StoreReadWriter, gid interfaces.GroupId) error {
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

// TODO(hpucha): Pass blessings along.
func (sd *syncDatabase) CreateSyncGroup(ctx *context.T, call rpc.ServerCall, sgName string, spec wire.SyncGroupSpec, myInfo wire.SyncGroupMemberInfo) error {
	err := store.RunInTransaction(sd.db.St(), func(st store.StoreReadWriter) error {

		// Check permissions on Database.
		if err := sd.db.CheckPermsInternal(ctx, call, st); err != nil {
			return err
		}

		// TODO(hpucha): Check prefix ACLs on all SG prefixes.
		// This may need another method on util.Database interface.

		// TODO(hpucha): Do some SGACL checking. Check creator
		// has Admin privilege.

		// Get this Syncbase's id.
		ss := sd.db.App().Service().Sync().(*syncService)
		name := fmt.Sprintf("%x", ss.id)

		// Instantiate sg. Add self as joiner.
		sg := &interfaces.SyncGroup{
			Id:          newSyncGroupId(),
			Name:        sgName,
			SpecVersion: newSyncGroupVersion(),
			Spec:        spec,
			Creator:     name,
			AppName:     sd.db.App().Name(),
			DbName:      sd.db.Name(),
			Status:      interfaces.SyncGroupStatusPublishPending,
			Joiners:     map[string]wire.SyncGroupMemberInfo{name: myInfo},
		}

		if err := addSyncGroup(ctx, st, sg); err != nil {
			return err
		}

		// TODO(hpucha): Bootstrap DAG/Genvector etc for syncing the SG metadata.

		// Take a snapshot of the data to bootstrap the SyncGroup.
		if err := bootstrapSyncGroup(st, spec.Prefixes); err != nil {
			return err
		}

		// TODO(hpucha): Add watch notification to signal SG creation.

		return nil
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
	err = store.RunInTransaction(sd.db.St(), func(st store.StoreReadWriter) error {
		// Ensure SG still exists.
		sg, err := getSyncGroupByName(ctx, st, sgName)
		if err != nil {
			return err
		}

		sg.Status = interfaces.SyncGroupStatusPublishRejected
		return setSGDataEntry(ctx, st, sg.Id, sg)
	})
	return err
}

// TODO(hpucha): Should this be generalized?
func splitPrefix(name string) (string, string) {
	parts := strings.SplitN(name, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return parts[0], ""
}

func bootstrapSyncGroup(tx store.StoreReadWriter, prefixes []string) error {
	_ = tx.(store.Transaction)

	for _, p := range prefixes {
		table, row := splitPrefix(p)
		it := tx.Scan(util.ScanRangeArgs(table, row, ""))
		key, value := []byte{}, []byte{}
		for it.Advance() {
			key, value = it.Key(key), it.Value(value)

			// TODO(hpucha): Ensure prefix ACLs show up in the scan
			// stream.

			// TODO(hpucha): Process this object.
		}
		if err := it.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (sd *syncDatabase) publishInMountTables(ctx *context.T, call rpc.ServerCall, spec wire.SyncGroupSpec) error {
	// TODO(hpucha): To be implemented.
	// Pass server to Service in store.
	// server.ServeDispatcher(*name, service, authorizer)
	return nil
}

////////////////////////////////////////////////////////////
// Methods for SyncGroup create/join between Syncbases.

func (s *syncService) PublishSyncGroup(ctx *context.T, call rpc.ServerCall, sg interfaces.SyncGroup) error {

	// Find the database store for this SyncGroup.
	app, err := s.sv.App(ctx, call, sg.AppName)
	if err != nil {
		return err
	}
	db, err := app.NoSQLDatabase(ctx, call, sg.DbName)
	if err != nil {
		return err
	}

	err = store.RunInTransaction(db.St(), func(st store.StoreReadWriter) error {
		localSG, err := getSyncGroupByName(ctx, st, sg.Name)

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
				return setSGDataEntry(ctx, st, localSG.Id, localSG)
			}
			return nil
		}

		// Publish the SyncGroup.

		// TODO(hpucha): Use some ACL check to allow/deny publishing.
		// TODO(hpucha): Ensure node is on Admin ACL.

		name := fmt.Sprintf("%x", s.id)
		// TODO(hpucha): Default priority?
		sg.Joiners[name] = wire.SyncGroupMemberInfo{}
		sg.Status = interfaces.SyncGroupStatusRunning
		return addSyncGroup(ctx, st, &sg)
	})

	return err
}

func (s *syncService) JoinSyncGroup(ctx *context.T, call rpc.ServerCall) error {
	return verror.NewErrNotImplemented(ctx)
}
