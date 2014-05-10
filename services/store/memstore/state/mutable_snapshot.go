package state

import (
	"errors"
	"fmt"

	"veyron/services/store/estore"
	"veyron/services/store/memstore/acl"
	"veyron/services/store/memstore/field"
	"veyron/services/store/memstore/refs"

	"veyron/runtimes/google/lib/functional"
	"veyron2/security"
	"veyron2/storage"
)

// MutableSnapshot is a mutable version of the snapshot.  It contains a Snapshot
// and a Mutations set.
//
// Reference counting is used to collect garbage that is no longer reachable
// using a pathname.  References can be cyclic, so the reference counting
// includes cycle detection.
//
// References never dangle.  This restricts what operations can be performed.
// It isn't allowed to add an object to the state that has a dangling reference;
// so if you want to set up a cycle atomically, you should add the objects to
// the state without references then mutate the objects to add the cyclic
// references.  This can be done in a single transaction, so that the
// intermediate state is not observable.
//
// TODO(jyh): Alternatively, we could relax the object operations so that
// objects can be added to a transaction with dangling references.  The
// references would still be checked at Commit time, aborting the transaction if
// dangling references are detected.  However, it would mean that intermediate
// states in the transaction would be inconsistent.  This might be fine, but we
// should decide whether transaction operations like Search() are allowed on
// these inconsistent states while a transaction is being constructed.  In the
// meantime, we keep the strict approach, where intermediate states are
// consistent.
type MutableSnapshot struct {
	snapshot

	// gcRoots contains the nodes that should be considered for garbage
	// collection.
	gcRoots map[storage.ID]struct{}

	// mutations is the current set of changes.
	mutations *Mutations

	// deletions is the current set of deletions.  The version is at
	// the point of deletion.
	deletions map[storage.ID]storage.Version
}

// Mutations represents a set of mutations to the state.  This is used to
// collect the operations in a transaction.
type Mutations struct {
	// Timestamp corresponds to the time that the mutations were applied to the
	// state.  It is set when applyMutations() was called.  The value is based
	// on Unix time, the number of nanoseconds elapsed since January 1, 1970
	// UTC.  However, it is monotonically increasing so that subsequent
	// mutations have increasing timestamps that differ by at least one.
	Timestamp uint64

	// RootID is the storage.ID of the root value.  Valid only if SetRootID is true.
	RootID    storage.ID
	SetRootID bool

	// Preconditions is the set of expected versions.
	Preconditions map[storage.ID]storage.Version

	// Delta is the set of changes.
	Delta map[storage.ID]*Mutation

	// Deletions contains the IDs for values that have been deleted.  The
	// version is taken from the time of deletion.  It is like a weak
	// precondition, where *if* the value exists, it should have the specified
	// version.  The target snapshot is allowed to perform garbage collection
	// too, so the deleted value is not required to exist.
	Deletions map[storage.ID]storage.Version
}

// mutation is an update to a single value in the state.
type Mutation struct {
	// Postcondition is the version after the mutation.
	Postcondition storage.Version

	// Value is the new value.
	Value interface{}

	// Tags are the new tags.
	//
	// TODO(jyh): Replace with a delta encoding.
	Tags storage.TagList

	// Dir is the set of new directory entries.
	//
	// TODO(jyh): Replace this with a delta, to support large directories.
	Dir []*storage.DEntry

	// Refs are the set of references in the Value and Dir.
	refs refs.Set
}

var (
	// TODO(tilaks): don't expose errors, use verror instead.
	ErrBadPath            = errors.New("malformed path")
	ErrTypeMismatch       = errors.New("type mismatch")
	ErrNotFound           = errors.New("not found")
	ErrBadRef             = errors.New("value has dangling references")
	ErrCantUnlinkByID     = errors.New("can't unlink entries by ID")
	ErrPreconditionFailed = errors.New("precondition failed")
	ErrIDsDoNotMatch      = errors.New("IDs do not match")
	ErrPermissionDenied   = errors.New("permission denied") // TODO(tilaks): can permission denied leak store structure?
	ErrNotTagList         = errors.New("not a TagList")

	nullID storage.ID
)

// newMutations returns a fresh Mutations set.
func newMutations() *Mutations {
	var m Mutations
	m.reset()
	return &m
}

// reset resets the Mutations state.
func (m *Mutations) reset() {
	m.Preconditions = make(map[storage.ID]storage.Version)
	m.Delta = make(map[storage.ID]*Mutation)
	m.Deletions = make(map[storage.ID]storage.Version)
}

// addPrecondition adds a precondition if it does not already exisn.
func (m *Mutations) addPrecondition(c *Cell) {
	// Set the precondition if not already set.  For cells that have been
	// created in the current Mutations/transaction, the value store in
	// m.Preconditions[c.id] will be zero, but c.version is the initial non-zero
	// version number, so we guard against the override.
	if _, ok := m.Preconditions[c.ID]; !ok {
		m.Preconditions[c.ID] = c.Version
	}
}

// UpdateRefs updates the refs field in the Mutation.
func (m *Mutation) UpdateRefs() {
	r := refs.NewBuilder()
	r.AddValue(m.Value)
	r.AddDEntries(m.Dir)
	m.refs = r.Get()
}

// newSnapshot returns an empty snapshot.
func newMutableSnapshot(admin security.PublicID) *MutableSnapshot {
	return &MutableSnapshot{
		snapshot:  newSnapshot(admin),
		gcRoots:   make(map[storage.ID]struct{}),
		mutations: newMutations(),
		deletions: make(map[storage.ID]storage.Version),
	}
}

// Mutations returns the set of mutations in the snapshot.
func (sn *MutableSnapshot) Mutations() *Mutations {
	return sn.mutations
}

// GetSnapshot create a readonly copy of the snapshot.
func (sn *MutableSnapshot) GetSnapshot() Snapshot {
	// Perform a GC to clear out gcRoots.
	sn.gc()
	cp := sn.snapshot
	cp.resetACLCache()
	return &cp
}

// deepCopy creates a copy of the snapshot.  Mutations to the copy do not affect
// the original, and vice versa.
func (sn *MutableSnapshot) deepCopy() *MutableSnapshot {
	// Perform a GC to clear out gcRoots.
	sn.gc()
	cp := *sn
	cp.mutations = newMutations()
	cp.gcRoots = make(map[storage.ID]struct{})
	cp.resetACLCache()
	return &cp
}

// deref performs a lookup based on storage.ID, panicing if the cell is not found.
// This is used internally during garbage collection when we can assume that
// there are no dangling references.
func (sn *MutableSnapshot) deref(id storage.ID) *Cell {
	c := sn.Find(id)
	if c == nil {
		panic(fmt.Sprintf("Dangling reference: %s", id))
	}

	// Copy the cell to ensure the original state is not modified.
	//
	// TODO(jyh): This can be avoided if the cell has already been copied in the
	// current transaction.
	cp := *c
	sn.idTable = sn.idTable.Put(&cp)
	return &cp
}

// delete removes the cell from the state.
func (sn *MutableSnapshot) delete(c *Cell) {
	sn.idTable = sn.idTable.Remove(c)
	sn.deletions[c.ID] = c.Version
	sn.aclCache.Invalidate(c.ID)
}

// put adds a cell to the state, also adding the new value to the Mutations set.
func (sn *MutableSnapshot) put(c *Cell) {
	mu := sn.mutations
	d := refs.FlattenDir(c.Dir)
	m, ok := mu.Delta[c.ID]
	if ok {
		m.Value = c.Value
		m.refs = c.refs
		m.Dir = d
		m.Tags = c.Tags
	} else {
		mu.Preconditions[c.ID] = c.Version
		m = &Mutation{
			Postcondition: storage.NewVersion(),
			Value:         c.Value,
			Dir:           d,
			Tags:          c.Tags,
			refs:          c.refs,
		}
		mu.Delta[c.ID] = m
	}
	c.Version = m.Postcondition
	sn.idTable = sn.idTable.Put(c)
	sn.aclCache.Invalidate(c.ID)
}

// add adds a new Value to the state, updating reference counts.  Fails if the
// new value contains dangling references.
func (sn *MutableSnapshot) add(parentChecker *acl.Checker, id storage.ID, v interface{}) (*Cell, error) {
	c := sn.Find(id)
	if c == nil {
		// There is no current value, so create a new cell for the value and add
		// it.
		//
		// There is no permissions check here because the caller is not modifying a preexisting value.
		//
		// TODO(jyh): However, the new value is created with default
		// permissions, which does not include the ability to set the tags on
		// the cell.  So the caller can wind up in a odd situation where they
		// can create a value, but not be able to read it back, and no way to
		// fix it.  Figure out whether this is a problem.
		c = &Cell{
			ID:       id,
			refcount: 0,
			Value:    v,
			Dir:      refs.EmptyDir,
			Tags:     storage.TagList{},
			inRefs:   refs.Empty,
			Version:  storage.NoVersion,
		}
		c.setRefs()
		if !sn.refsExist(c.refs) {
			return nil, ErrBadRef
		}
		sn.put(c)
		sn.addRefs(id, c.refs)
		return c, nil
	}

	// There is already a value in the state, so replace it with the new value.
	checker := parentChecker.Copy()
	checker.Update(c.Tags)
	return sn.replaceValue(checker, c, v)
}

// replaceValue updates the cell.value.
func (sn *MutableSnapshot) replaceValue(checker *acl.Checker, c *Cell, v interface{}) (*Cell, error) {
	if !checker.IsAllowed(security.WriteLabel) {
		return nil, ErrPermissionDenied
	}
	cp := *c
	cp.Value = v
	cp.setRefs()
	if !sn.refsExist(cp.refs) {
		return nil, ErrBadRef
	}
	sn.put(&cp)
	sn.updateRefs(c.ID, c.refs, cp.refs)
	return &cp, nil
}

// replaceDir updates the cell.dir.
func (sn *MutableSnapshot) replaceDir(checker *acl.Checker, c *Cell, d functional.Set) (*Cell, error) {
	if !checker.IsAllowed(security.WriteLabel) {
		return nil, ErrPermissionDenied
	}
	cp := *c
	cp.Dir = d
	cp.setRefs()
	if !sn.refsExist(cp.refs) {
		return nil, ErrBadRef
	}
	sn.put(&cp)
	sn.updateRefs(c.ID, c.refs, cp.refs)
	return &cp, nil
}

// replaceTags replaces the cell.tags.
func (sn *MutableSnapshot) replaceTags(checker *acl.Checker, c *Cell, tags storage.TagList) (*Cell, error) {
	if !checker.IsAllowed(security.AdminLabel) {
		return nil, ErrPermissionDenied
	}
	cp := *c
	cp.Tags = tags
	cp.setRefs()
	if !sn.refsExist(cp.refs) {
		return nil, ErrBadRef
	}
	sn.put(&cp)
	sn.updateRefs(c.ID, c.refs, cp.refs)
	return &cp, nil
}

// resolveCell performs a path-based lookup, traversing the state from the
// root.
//
// Returns (cell, suffix, v), where cell contains the value, suffix is the path
// to the value, v is the value itself.  If the operation failed, the returned
// cell is nil.
func (sn *MutableSnapshot) resolveCell(checker *acl.Checker, path storage.PathName, mu *Mutations) (*Cell, storage.PathName, interface{}) {
	// Get the starting object.
	id, suffix, ok := path.GetID()
	if ok {
		path = suffix
		// Remove garbage so that only valid uids are visible.
		sn.gc()
		checker.Update(uidTagList)
	} else {
		id = sn.rootID
	}

	return sn.resolve(checker, id, path, mu)
}

// Get returns the value for a path.
func (sn *MutableSnapshot) Get(pid security.PublicID, path storage.PathName) (*storage.Entry, error) {
	checker := sn.newPermChecker(pid)
	cell, suffix, v := sn.resolveCell(checker, path, sn.mutations)
	if cell == nil {
		return nil, ErrNotFound
	}
	var e *storage.Entry
	if len(suffix) == 0 {
		e = cell.getEntry()
	} else {
		e = newSubfieldEntry(v)
	}
	return e, nil
}

// Put adds a new value to the state or replaces an existing one.  Returns
// the *Stat for the enclosing *cell.
func (sn *MutableSnapshot) Put(pid security.PublicID, path storage.PathName, v interface{}) (*storage.Stat, error) {
	checker := sn.newPermChecker(pid)
	c, err := sn.putValueByPath(checker, path, v)
	if err != nil {
		return nil, err
	}
	return c.getStat(), nil
}

func (sn *MutableSnapshot) putValueByPath(checker *acl.Checker, path storage.PathName, v interface{}) (*Cell, error) {
	v = deepcopy(v)

	if path.IsRoot() {
		return sn.putRoot(checker, v)
	}
	if id, suffix, ok := path.GetID(); ok && len(suffix) == 0 {
		return sn.putValueByID(checker, id, v)
	}
	return sn.putValue(checker, path, v)
}

// putValue is called for a normal Put() operation, where a new value is being
// added, and as a consequence the containing "parent" value is being modified.
// There are two cases: 1) the value <v> is written directly into the parent, or
// 2) the field has type storage.ID.  In the latter case, the <id> is assigned
// into the parent, and the value id->v is added to the idTable.
func (sn *MutableSnapshot) putValue(checker *acl.Checker, path storage.PathName, v interface{}) (*Cell, error) {
	// Find the parent object.
	c, suffix, _ := sn.resolveCell(checker, path[:len(path)-1], sn.mutations)
	if c == nil {
		return nil, ErrNotFound
	}
	if len(suffix) > 0 && suffix[0] == refs.TagsDirName {
		return sn.putTagsValue(checker, path, suffix[1:], c, v)
	}
	value := deepcopy(c.Value)
	p, s := field.Get(value, suffix)
	if len(s) != 0 {
		return nil, ErrNotFound
	}

	// Add value to the parent.
	name := path[len(path)-1]
	result, id := field.Set(p, name, v)
	switch result {
	case field.SetFailed:
		if len(suffix) != 0 {
			return nil, ErrNotFound
		}
		if name == refs.TagsDirName {
			return sn.putTags(checker, c, v)
		}
		return sn.putDirEntry(checker, c, name, v)
	case field.SetAsID:
		nc, err := sn.add(checker, id, v)
		if err != nil {
			return nil, err
		}
		// The sn.add may have modified the cell, so fetch it again.
		if _, err = sn.replaceValue(checker, sn.Find(c.ID), value); err != nil {
			return nil, err
		}
		return nc, nil
	case field.SetAsValue:
		return sn.replaceValue(checker, c, value)
	}
	panic("not reached")
}

// putTagsValue modifies the cell.tags value.
func (sn *MutableSnapshot) putTagsValue(checker *acl.Checker, path, suffix storage.PathName, c *Cell, v interface{}) (*Cell, error) {
	tags := deepcopy(c.Tags).(storage.TagList)
	p, s := field.Get(&tags, suffix)
	if len(s) != 0 {
		return nil, ErrNotFound
	}

	// Add value to the parent.
	name := path[len(path)-1]
	result, id := field.Set(p, name, v)
	switch result {
	case field.SetFailed:
		return nil, ErrNotFound
	case field.SetAsID:
		nc, err := sn.add(checker, id, v)
		if err != nil {
			return nil, err
		}
		// The sn.add may have modified the cell, so fetch it again.
		if _, err = sn.replaceTags(checker, sn.Find(c.ID), tags); err != nil {
			return nil, err
		}
		return nc, nil
	case field.SetAsValue:
		return sn.replaceTags(checker, c, tags)
	}
	panic("not reached")
}

// putTags updates the tags.
func (sn *MutableSnapshot) putTags(checker *acl.Checker, c *Cell, v interface{}) (*Cell, error) {
	tags, ok := v.(storage.TagList)
	if !ok {
		return nil, ErrNotTagList
	}
	return sn.replaceTags(checker, c, tags)
}

// putDirEntry replaces or adds a directory entry.
func (sn *MutableSnapshot) putDirEntry(checker *acl.Checker, c *Cell, name string, v interface{}) (*Cell, error) {
	r := &refs.Ref{Path: refs.NewSingletonPath(name), Label: security.ReadLabel}
	x, ok := c.Dir.Get(r)
	if !ok {
		var ncell *Cell
		if id, ok := v.(storage.ID); ok {
			// The entry is a hard link.
			r.ID = id
			ncell = sn.Find(id)
		} else {
			// The entry does not exist yet; create it.
			var err error
			id := storage.NewID()
			ncell, err = sn.add(checker, id, v)
			if err != nil {
				return nil, err
			}
			r.ID = id
			// The sn.add may have modified the cell, so fetch it again.
			c = sn.Find(c.ID)
		}
		dir := c.Dir.Put(r)
		if _, err := sn.replaceDir(checker, c, dir); err != nil {
			return nil, err
		}
		return ncell, nil
	}

	// Replace the existing value.
	return sn.add(checker, x.(*refs.Ref).ID, v)
}

// putRoot replaces the root.
func (sn *MutableSnapshot) putRoot(checker *acl.Checker, v interface{}) (*Cell, error) {
	if !checker.IsAllowed(security.WriteLabel) {
		return nil, ErrPermissionDenied
	}

	id := sn.rootID
	c := sn.Find(id)
	if c == nil {
		id = storage.NewID()
	}

	// Add the new element.
	ncell, err := sn.add(checker, id, v)
	if err != nil {
		return nil, err
	}

	// Redirect the rootID.
	if c == nil {
		sn.ref(id)
		sn.rootID = id
		sn.mutations.RootID = id
		sn.mutations.SetRootID = true
	}
	return ncell, nil
}

// putValueByID replaces a value referred to by ID.
func (sn *MutableSnapshot) putValueByID(checker *acl.Checker, id storage.ID, v interface{}) (*Cell, error) {
	checker.Update(uidTagList)
	if !checker.IsAllowed(security.WriteLabel) {
		return nil, ErrPermissionDenied
	}

	sn.gc()
	return sn.add(checker, id, v)
}

// Remove removes a value.
func (sn *MutableSnapshot) Remove(pid security.PublicID, path storage.PathName) error {
	checker := sn.newPermChecker(pid)
	if path.IsRoot() {
		if !checker.IsAllowed(security.WriteLabel) {
			return ErrPermissionDenied
		}
		sn.unref(sn.rootID)
		sn.rootID = nullID
		sn.mutations.RootID = nullID
		sn.mutations.SetRootID = true
		return nil
	}
	if path.IsStrictID() {
		return ErrCantUnlinkByID
	}

	// Split the names into directory and field parts.
	cell, suffix, _ := sn.resolveCell(checker, path[:len(path)-1], sn.mutations)
	if cell == nil {
		return ErrNotFound
	}

	// Remove the field.
	name := path[len(path)-1]
	if name == refs.TagsDirName {
		_, err := sn.replaceTags(checker, cell, storage.TagList{})
		return err
	}
	r := &refs.Ref{Path: refs.NewSingletonPath(name), Label: security.ReadLabel}
	if cell.Dir.Contains(r) {
		_, err := sn.replaceDir(checker, cell, cell.Dir.Remove(r))
		return err
	}
	value := deepcopy(cell.Value)
	p, _ := field.Get(value, suffix)
	if !field.Remove(p, name) {
		return ErrNotFound
	}

	_, err := sn.replaceValue(checker, cell, value)
	return err
}

// PutMutations puts some externally constructed mutations. Does not update
// cells or refs, so regular Puts, Gets and Removes may be inconsistent.
func (sn *MutableSnapshot) PutMutations(extmu []estore.Mutation) {
	mu := sn.mutations
	for _, extm := range extmu {
		id := extm.ID
		// If the object has no version, it was deleted.
		if extm.Version == storage.NoVersion {
			mu.Deletions[id] = extm.PriorVersion
			if extm.IsRoot {
				mu.SetRootID = true
				mu.RootID = nullID
			}
			continue
		}
		if extm.IsRoot {
			mu.SetRootID = true
			mu.RootID = id
		}
		mu.Preconditions[id] = extm.PriorVersion
		m := &Mutation{
			Postcondition: extm.Version,
			Value:         extm.Value,
			Dir:           unflattenDir(extm.Dir),
		}
		m.UpdateRefs()
		mu.Delta[id] = m
	}
}

// TODO(tilaks): revisit when vsync.Mutation.Dir is of type []*storage.DEntry
// (once we support optional structs in the idl).
func unflattenDir(fdir []storage.DEntry) []*storage.DEntry {
	pdir := make([]*storage.DEntry, len(fdir))
	for i, _ := range fdir {
		pdir[i] = &fdir[i]
	}
	return pdir
}
