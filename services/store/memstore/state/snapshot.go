package state

import (
	"reflect"

	"veyron/services/store/memstore/acl"
	"veyron/services/store/memstore/field"
	"veyron/services/store/memstore/refs"

	"veyron2/security"
	"veyron2/storage"
)

type Snapshot interface {
	// NewIterator returns an Iterator that starts with the value at <path>.
	// pathFilter is used to automatically limit traversal of certain paths.
	// If filter is given, it is used to limit traversal beneath certain paths
	// and limit the results of the iteration. If filter is nil, all decendents
	// of the specified path are returned.
	NewIterator(pid security.PublicID, path storage.PathName, pathFilter PathFilter, filter IterFilter) Iterator

	// PathMatch returns true iff there is a name for the store value that
	// matches the pathRegex.
	PathMatch(pid security.PublicID, id storage.ID, regex *PathRegex) bool

	// Find performs a lookup based on storage.ID, returning nil if the cell is not found.
	Find(id storage.ID) *Cell

	// Get returns the value for a path.
	Get(pid security.PublicID, path storage.PathName) (*storage.Entry, error)
}

// Snapshot keeps the state for the store.  The snapshot contains a dictionary
// and a root,
//
//    idTable : storage.ID -> storage.Value
//    rootID : storage.ID
//
// Snapshots support isolation by using a functional/immutable dictionary for the
// idTable.
//
// Paths are resolved by traversing the snapshot from the root, using reflection to
// traverse fields within each of the values.  For example, to resolve a path
// /a/b/c/d/e/f/g/h, we perform the following steps.
//
//     id1 := idTable[rootID].a.b.c
//     id2 := idTable[id1].d.e
//     id3 := idTable[id2].f.g.h
//     return id3
//
// If any of those resolution steps fails (if the idTable doesn't contain an
// entry, or a field path like .a.b.c doesn't exist), then the resolution fails.
type snapshot struct {
	// idTable is the dictionary of values.  We use functional sets to make it
	// easy to perform snapshotting.
	idTable cellSet

	// rootID is the identifier of the root object.
	rootID storage.ID

	// aclCache caches a set of ACLs.
	aclCache acl.Cache

	// defaultACLSet is the ACLSet used to access the root directory.
	defaultACLSet acl.Set
}

// newSnapshot returns an empty snapshot.
func newSnapshot(admin security.PublicID) snapshot {
	sn := snapshot{
		idTable:       emptyIDTable,
		defaultACLSet: makeDefaultACLSet(admin),
	}
	sn.aclCache = acl.NewCache(sn.makeFindACLFunc())
	return sn
}

// resetACLCache resets the aclCache.
func (sn *snapshot) resetACLCache() {
	sn.aclCache.UpdateFinder(sn.makeFindACLFunc())
}

// Find performs a lookup based on storage.ID, returning nil if the cell is not found.
func (sn *snapshot) Find(id storage.ID) *Cell {
	v, ok := sn.idTable.Get(&Cell{ID: id})
	if !ok {
		return nil
	}
	return v.(*Cell)
}

// Get implements the Snapshot method.
func (sn *snapshot) Get(pid security.PublicID, path storage.PathName) (*storage.Entry, error) {
	checker := sn.newPermChecker(pid)
	// Pass nil for 'mutations' since the snapshot is immutable.
	cell, suffix, v := sn.resolveCell(checker, path, nil)
	if cell == nil {
		return nil, ErrNotFound
	}
	var e *storage.Entry
	if len(suffix) == 0 {
		e = cell.GetEntry()
	} else {
		e = newSubfieldEntry(v)
	}
	return e, nil
}

// resolveCell performs a path-based lookup, traversing the state from the
// root.
//
// Returns (cell, suffix, v), where cell contains the value, suffix is the path
// to the value, v is the value itself.  If the operation failed, the returned
// cell is nil.
func (sn *snapshot) resolveCell(checker *acl.Checker, path storage.PathName, mu *Mutations) (*Cell, storage.PathName, interface{}) {
	// Get the starting object.
	id, suffix, ok := path.GetID()
	if ok {
		path = suffix
		checker.Update(uidTagList)
	} else {
		id = sn.rootID
	}

	return sn.resolve(checker, id, path, mu)
}

func (sn *snapshot) resolve(checker *acl.Checker, id storage.ID, path storage.PathName, mu *Mutations) (*Cell, storage.PathName, interface{}) {
	cell := sn.Find(id)
	if cell == nil {
		return nil, nil, nil
	}
	for {
		if mu != nil {
			mu.addPrecondition(cell)
		}
		checker.Update(cell.Tags)
		var v reflect.Value
		var suffix storage.PathName
		if len(path) > 0 && path[0] == refs.TagsDirName {
			if !checker.IsAllowed(security.AdminLabel) {
				// Access to .tags requires admin priviledges.
				return nil, nil, ErrPermissionDenied
			}
			v, suffix = field.Get(cell.Tags, path[1:])
		} else {
			if !checker.IsAllowed(security.ReadLabel) {
				// Do not return errPermissionDenied because that would leak the
				// existence of the inaccessible value.
				return nil, nil, nil
			}
			v, suffix = field.Get(cell.Value, path)
		}
		x := v.Interface()
		if id, ok := x.(storage.ID); ok {
			// Always dereference IDs.
			cell = sn.Find(id)
			path = suffix
			continue
		}
		switch len(suffix) {
		case 0:
			// The path is fully resolved.  We're done.
			return cell, path, x
		case len(path):
			// The path couldn't be resolved at all.  It must be an entry in the
			// implicit directory.
			r, ok := cell.Dir.Get(&refs.Ref{Path: refs.NewSingletonPath(path[0])})
			if !ok {
				return nil, nil, nil
			}
			cell = sn.Find(r.(*refs.Ref).ID)
			path = path[1:]
		default:
			// The path is partially resolved, but it does not resolve to a
			// storage.ID.  This is an error.
			return nil, nil, nil
		}
	}
}
