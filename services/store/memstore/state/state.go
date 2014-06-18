package state

import (
	"time"

	"veyron/services/store/memstore/refs"

	"veyron2/security"
	"veyron2/storage"
)

type State struct {
	snapshot *MutableSnapshot

	// timestamp is the time of the last mutation applied, in nanoseconds since
	// the epoch.  See comment for snapshot.Mutations.Timestamp.
	timestamp uint64
}

// refUpdate represents a reference change to a value.
type refUpdate struct {
	id     storage.ID
	before refs.Set
	after  refs.Set
}

// New returns an empty State.
func New(admin security.PublicID) *State {
	return &State{snapshot: newMutableSnapshot(admin)}
}

// Timestamp returns the timestamp of the latest mutation to the state in
// nanoseconds.
func (st *State) Timestamp() uint64 {
	return st.timestamp
}

// DeepCopy creates a copy of the state.  Mutations to the copy do not affect
// the original, and vice versa.
func (st *State) DeepCopy() *State {
	return &State{st.MutableSnapshot(), st.timestamp}
}

// GC performs a manual garbage collection.
func (st *State) GC() {
	st.snapshot.gc()
}

// Snapshot returns a read-only copy of the state.
func (st *State) Snapshot() Snapshot {
	return st.snapshot.GetSnapshot()
}

// MutableSnapshot creates a copy of the state.  Mutations to the copy do not
// affect the original, and vice versa.
func (st *State) MutableSnapshot() *MutableSnapshot {
	return st.snapshot.deepCopy()
}

// Deletions returns the set of IDs for values that have been deleted from
// the state.  Returns nil iff there have been no deletions.
func (st *State) Deletions() *Mutations {
	if len(st.snapshot.deletions) == 0 {
		return nil
	}

	// Package the deletions into a transaction.
	var mu Mutations
	ts := st.timestamp + 1
	mu.Timestamp = ts
	mu.Deletions = st.snapshot.deletions
	st.timestamp = ts
	st.snapshot.deletions = make(map[storage.ID]storage.Version)
	return &mu
}

// ApplyMutations applies a set of mutations atomically.
//
// We don't need to check permissions because:
//    1. Permissions were checked as the mutations were created.
//    2. Preconditions ensure that all paths to modified values haven't changed.
//    3. The client cannot fabricate a mutations value.
func (st *State) ApplyMutations(mu *Mutations) error {
	// Assign a timestamp.
	ts := uint64(time.Now().UnixNano())
	if ts <= st.timestamp {
		ts = st.timestamp + 1
	}
	mu.Timestamp = ts

	if err := st.snapshot.applyMutations(mu); err != nil {
		return err
	}

	st.timestamp = ts
	return nil
}

func (sn *MutableSnapshot) applyMutations(mu *Mutations) error {
	// Check the preconditions.
	table := sn.idTable
	for id, pre := range mu.Preconditions {
		c, ok := table.Get(&Cell{ID: id})
		// If the precondition is 0, it means that the cell is being created,
		// and it must not already exist.  We get a precondition failure if pre
		// is 0 and the cell already exists, or pre is not 0 and the cell does
		// not already exist or have the expected version.
		if pre == 0 && ok || pre != 0 && (!ok || c.(*Cell).Version != pre) {
			return ErrPreconditionFailed
		}
	}
	for id, pre := range mu.Deletions {
		c, ok := table.Get(&Cell{ID: id})
		// The target is not required to exist.
		if ok && c.(*Cell).Version != pre {
			return ErrPreconditionFailed
		}
	}

	// Changes to the state begin now. These changes should not fail,
	// as we don't support rollback.

	// Apply the mutations.
	updates := make([]*refUpdate, 0, len(mu.Delta))
	for id, m := range mu.Delta {
		d := refs.BuildDir(m.Dir)
		cl, ok := table.Get(&Cell{ID: id})
		sn.aclCache.Invalidate(id)
		if !ok {
			c := &Cell{
				ID:      id,
				Version: m.Postcondition,
				Value:   m.Value,
				Dir:     d,
				Tags:    m.Tags,
				refs:    m.refs,
				inRefs:  refs.Empty,
			}
			table = table.Put(c)
			updates = append(updates, &refUpdate{id: c.ID, before: refs.Empty, after: c.refs})
		} else {
			c := cl.(*Cell)
			cp := *c
			cp.Version = m.Postcondition
			cp.Value = m.Value
			cp.Dir = d
			cp.Tags = m.Tags
			cp.refs = m.refs
			table = table.Put(&cp)
			updates = append(updates, &refUpdate{id: c.ID, before: c.refs, after: cp.refs})
		}
	}
	sn.idTable = table

	// Add the refs.
	for _, u := range updates {
		sn.updateRefs(u.id, u.before, u.after)
	}

	// Redirect the rootID.
	if mu.SetRootID && mu.RootID != sn.rootID {
		if mu.RootID != nullID {
			sn.ref(mu.RootID)
		}
		if sn.rootID != nullID {
			sn.unref(sn.rootID)
		}
		sn.rootID = mu.RootID
	}

	return nil
}
