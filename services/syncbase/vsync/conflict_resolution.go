// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"v.io/v23/context"
	"v.io/v23/verror"
	"v.io/x/lib/vlog"
)

// Policies for conflict resolution.
// TODO(hpucha): Move relevant parts to client-facing vdl.
const (
	// Resolves conflicts by picking the update with the most recent timestamp.
	useTime = iota

	// TODO(hpucha): implement other policies.
	// Resolves conflicts by using the app conflict resolver callbacks via store.
	useCallback
)

var (
	// conflictResolutionPolicy is the policy used to resolve conflicts.
	conflictResolutionPolicy = useTime
)

// resolutionType represents how a conflict is resolved.
type resolutionType byte

const (
	pickLocal  resolutionType = iota // local update was chosen as the resolution.
	pickRemote                       // remote update was chosen as the resolution.
	createNew                        // new update was created as the resolution.
)

// conflictResolution represents the state of a conflict resolution.
type conflictResolution struct {
	ty  resolutionType
	rec *localLogRec // Valid only if ty == createNew.
	val []byte       // Valid only if ty == createNew.
}

// resolveConflicts resolves conflicts for updated objects. Conflicts may be
// resolved by adding new versions or picking either the local or the remote
// version.
func (iSt *initiationState) resolveConflicts(ctx *context.T) error {
	for obj, st := range iSt.updObjects {
		if !st.isConflict {
			continue
		}

		// TODO(hpucha): Look up policy from the schema. Currently,
		// hardcoded to time.
		var err error
		st.res, err = iSt.resolveObjConflict(ctx, obj, st.oldHead, st.newHead, st.ancestor)
		if err != nil {
			return err
		}
	}

	return nil
}

// resolveObjConflict resolves a conflict for an object given its ID and the 3
// versions that express the conflict: the object's local version, its remote
// version (from the device contacted), and the closest common ancestor (see
// dag.go on how the ancestor is chosen). The function returns the new object
// value according to the conflict resolution policy.
func (iSt *initiationState) resolveObjConflict(ctx *context.T, oid, local, remote, ancestor string) (*conflictResolution, error) {
	// Fetch the log records of the 3 object versions.
	versions := []string{local, remote, ancestor}
	lrecs, err := iSt.getLogRecsBatch(ctx, oid, versions)
	if err != nil {
		return nil, err
	}

	// Resolve the conflict according to the resolution policy.
	switch conflictResolutionPolicy {
	case useTime:
		return iSt.resolveObjConflictByTime(ctx, oid, lrecs[0], lrecs[1], lrecs[2])
	default:
		return nil, verror.New(verror.ErrInternal, ctx, "unknown conflict resolution policy", conflictResolutionPolicy)
	}
}

// resolveObjConflictByTime resolves conflicts using the timestamps of the
// conflicting mutations.  It picks a mutation with the larger timestamp,
// i.e. the most recent update.  If the timestamps are equal, it uses the
// mutation version numbers as a tie-breaker, picking the mutation with the
// larger version.  Instead of creating a new version that resolves the
// conflict, we pick an existing version as the conflict resolution.
func (iSt *initiationState) resolveObjConflictByTime(ctx *context.T, oid string, local, remote, ancestor *localLogRec) (*conflictResolution, error) {
	var res conflictResolution
	switch {
	case local.Metadata.UpdTime.After(remote.Metadata.UpdTime):
		res.ty = pickLocal
	case local.Metadata.UpdTime.Before(remote.Metadata.UpdTime):
		res.ty = pickRemote
	case local.Metadata.CurVers > remote.Metadata.CurVers:
		res.ty = pickLocal
	case local.Metadata.CurVers < remote.Metadata.CurVers:
		res.ty = pickRemote
	default:
		vlog.Fatalf("sync: resolveObjConflictByTime: local and remote update times and versions are the same, local %v remote %v", local, remote)
	}

	return &res, nil
}

// getLogRecsBatch gets the log records for an array of versions for a given object.
func (iSt *initiationState) getLogRecsBatch(ctx *context.T, obj string, versions []string) ([]*localLogRec, error) {
	lrecs := make([]*localLogRec, len(versions))
	for p, v := range versions {
		logKey, err := getLogRecKey(ctx, iSt.tx, obj, v)
		if err != nil {
			return nil, err
		}
		dev, gen, err := splitLogRecKey(ctx, logKey)
		if err != nil {
			return nil, err
		}
		lrecs[p], err = getLogRec(ctx, iSt.tx, dev, gen)
		if err != nil {
			return nil, err
		}
	}
	return lrecs, nil
}
