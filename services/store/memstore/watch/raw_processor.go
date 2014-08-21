package watch

import (
	"veyron/services/store/memstore/refs"
	"veyron/services/store/memstore/state"
	"veyron/services/store/raw"

	"veyron2/security"
	"veyron2/services/watch/types"
	"veyron2/storage"
	"veyron2/verror"
)

var (
	rootPath = storage.ParsePath("/")
	nullID   storage.ID
)

// rawProcessor processes log entries into mutations.
type rawProcessor struct {
	// st is true iff the initial state has been processed.
	hasProcessedState bool
	// pid is the identity of the client watching for changes.
	pid security.PublicID
	// rootID is the id of the root object after processing a change.
	rootID storage.ID
	// rootVersion is the version of the store root after processing a change.
	rootVersion storage.Version
	// preparedDeletions is the set of ids for which deletion changes have been
	// sent by watch, but deleted entries have not been processed from the log.
	// This set consists of deleted store roots, because
	// 1) A root deletion is propagated as a deletion change on the root.
	// 2) A root deletion must be propagated immediately.
	// 3) GC is lazy, so we aggressively create a deletion change for the root.
	// An id is removed from preparedDeletions when the corresponding deleted
	// entry is processed from the log.
	preparedDeletions map[storage.ID]bool
}

func newRawProcessor(pid security.PublicID) (reqProcessor, error) {
	return &rawProcessor{
		hasProcessedState: false,
		pid:               pid,
		preparedDeletions: make(map[storage.ID]bool),
	}, nil
}

func (p *rawProcessor) processState(st *state.State) ([]types.Change, error) {
	// Check that the initial state has not already been processed.
	if p.hasProcessedState {
		return nil, errInitialStateAlreadyProcessed
	}
	p.hasProcessedState = true

	sn := st.MutableSnapshot()

	rootID, err := rootID(p.pid, sn)
	if err != nil {
		return nil, err
	}
	p.rootID = rootID

	var changes []types.Change

	// Create a change for each id in the state. In each change, the object
	// exists, has no PriorVersion, has the Version of the new cell, and
	// has the Value, Tags and Dir of the new cell.
	for it := sn.NewIterator(p.pid, nil,
		state.ListObjects, state.RecursiveFilter); it.IsValid(); it.Next() {

		entry := it.Get()
		id := entry.Stat.ID
		// Retrieve Value, Tags and Dir from the corresponding cell.
		cell := sn.Find(id)
		// If this object is the root, update rootVersion.
		isRoot := id == p.rootID
		if isRoot {
			p.rootVersion = cell.Version
		}
		value := &raw.Mutation{
			ID:           id,
			PriorVersion: storage.NoVersion,
			Version:      cell.Version,
			IsRoot:       isRoot,
			Value:        cell.Value,
			Dir:          flattenDir(refs.FlattenDir(cell.Dir)),
		}
		change := types.Change{
			State: types.Exists,
			Value: value,
		}
		// TODO(tilaks): don't clone change
		changes = append(changes, change)
	}
	return changes, nil
}

func (p *rawProcessor) processTransaction(mus *state.Mutations) ([]types.Change, error) {
	// Ensure that the initial state has been processed.
	if !p.hasProcessedState {
		return nil, errInitialStateNotProcessed
	}

	// If the root was deleted, add extra space for a prepared deletion.
	extra := 0
	if mus.SetRootID && !mus.RootID.IsValid() {
		extra = 1
	}
	changes := make([]types.Change, 0, len(mus.Delta)+len(mus.Deletions)+extra)

	if mus.SetRootID {
		if mus.RootID.IsValid() {
			p.rootID = mus.RootID
		} else {
			// The root was deleted, prepare a deletion change.
			value := &raw.Mutation{
				ID:           p.rootID,
				PriorVersion: p.rootVersion,
				Version:      storage.NoVersion,
				IsRoot:       true,
			}
			// TODO(tilaks): don't clone value.
			change := types.Change{
				State: types.DoesNotExist,
				Value: value,
			}
			changes = append(changes, change)

			p.preparedDeletions[p.rootID] = true
			p.rootID = nullID
			p.rootVersion = storage.NoVersion
		}
	}

	// Create a change for each mutation. In each change, the object exists,
	// has the PriorVersion, Version, Value, Tags and Dir specified in
	// the mutation.
	for id, mu := range mus.Delta {
		// If this object is the root, update rootVersion.
		isRoot := id == p.rootID
		if isRoot {
			p.rootVersion = mu.Postcondition
		}
		value := &raw.Mutation{
			ID:           id,
			PriorVersion: mus.Preconditions[id],
			Version:      mu.Postcondition,
			IsRoot:       isRoot,
			Value:        mu.Value,
			Dir:          flattenDir(mu.Dir),
		}
		// TODO(tilaks): don't clone value.
		change := types.Change{
			State: types.Exists,
			Value: value,
		}
		// TODO(tilaks): don't clone change.
		changes = append(changes, change)
	}
	// Create a change for each deletion (if one has not already been prepared).
	// In each change, the object does not exist, has the specified PriorVersion,
	// has no Version, and has nil Value, Tags and Dir.
	for id, precondition := range mus.Deletions {
		if p.preparedDeletions[id] {
			delete(p.preparedDeletions, id)
			continue
		}
		value := &raw.Mutation{
			ID:           id,
			PriorVersion: precondition,
			Version:      storage.NoVersion,
			IsRoot:       false,
		}
		// TODO(tilaks): don't clone value.
		change := types.Change{
			State: types.DoesNotExist,
			Value: value,
		}
		// TODO(tilaks): don't clone change.
		changes = append(changes, change)
	}
	return changes, nil
}

// TODO(tilaks): revisit when raw.Mutation.Dir is of type []*storage.DEntry
// (once we support optional structs in the idl).
func flattenDir(pdir []*storage.DEntry) []storage.DEntry {
	fdir := make([]storage.DEntry, len(pdir))
	for i, p := range pdir {
		fdir[i] = *p
	}
	return fdir
}

// rootID returns the id of the root object in the snapshot. If the snapshot
// does not have a root, nullID is returned.
func rootID(pid security.PublicID, sn *state.MutableSnapshot) (storage.ID, error) {
	entry, err := sn.Get(pid, rootPath)
	if verror.Is(err, verror.NotFound) {
		return nullID, nil
	}
	if err != nil {
		return nullID, err
	}
	return entry.Stat.ID, nil
}
