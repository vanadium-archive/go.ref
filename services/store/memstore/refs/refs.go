// Package refs represents references from one value to another.
package refs

import (
	"veyron/runtimes/google/lib/functional"
	"veyron/runtimes/google/lib/functional/rb"
	"veyron/services/store/raw"

	"veyron2/storage"
)

// Set is a set of *Ref values.
type Set functional.Set // *Ref

// Ref represents a single reference in a store value.  It includes the
// storage.ID, and the path to the reference.
type Ref struct {
	ID   storage.ID
	Path *Path
}

// Dir represents a directory, which is a set of *Ref, sorted by path.
type Dir functional.Set // *Ref

var (
	Empty    Set = rb.NewSet(compareRefs)
	EmptyDir Dir = rb.NewSet(compareRefsByPath)
)

// *ref values are sorted lexicoigraphically by (id, path).
func compareRefs(it1, it2 interface{}) bool {
	r1 := it1.(*Ref)
	r2 := it2.(*Ref)
	cmp := storage.CompareIDs(r1.ID, r2.ID)
	return cmp < 0 || (cmp == 0 && ComparePaths(r1.Path, r2.Path) < 0)
}

// compareRefsByPath compares refs using their Path.
func compareRefsByPath(a, b interface{}) bool {
	return ComparePaths(a.(*Ref).Path, b.(*Ref).Path) < 0
}

// FlattenDir flattens the directory map into an association list.
func FlattenDir(d Dir) []*raw.DEntry {
	l := make([]*raw.DEntry, d.Len())
	i := 0
	d.Iter(func(v interface{}) bool {
		r := v.(*Ref)
		l[i] = &raw.DEntry{ID: r.ID, Name: r.Path.hd}
		i++
		return true
	})
	return l
}

// BuildDir builds a Dir from the association list.
func BuildDir(l []*raw.DEntry) Dir {
	d := EmptyDir
	for _, de := range l {
		d = d.Put(&Ref{ID: de.ID, Path: NewSingletonPath(de.Name)})
	}
	return d
}
