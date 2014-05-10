package state

import (
	"bytes"

	"veyron/services/store/memstore/refs"

	// TODO(cnicolaou): mv lib/functional into veyron somewhere.
	"veyron/runtimes/google/lib/functional"
	"veyron/runtimes/google/lib/functional/rb"
	"veyron2/storage"
)

// cell represents an entry in the store.  It is reference-counted, and it
// contains the actual value.
type Cell struct {
	// Unique ID for the cell.
	ID storage.ID

	// Value
	Value interface{}

	// Implicit directory.
	Dir refs.Set

	// tags are the access control tags.
	Tags storage.TagList

	// refs includes the references in the value, the dir, and the tags.
	//
	// TODO(jyh): This is simple, but it would be more space efficient to
	// include only the refs in the value, or drop this field entirely.
	refs refs.Set

	// inRefs contains all the incoming references -- that is, references
	// from other values to this one.
	inRefs refs.Set

	// TODO(jyh): The following fields can be packed into a single word.
	refcount uint
	color    color
	buffered bool

	// version is the version number.
	Version storage.Version

	// TODO(jyh): Add stat info and attributes.
}

// cellSet is a functional map from  storage.ID to *cell.
type cellSet functional.Set

var (
	emptyIDTable cellSet = rb.NewSet(compareCellsByID)
)

// compareCellsByID compares the two cells' IDs.
func compareCellsByID(a, b interface{}) bool {
	return bytes.Compare(a.(*Cell).ID[:], b.(*Cell).ID[:]) < 0
}

// newSubfieldEntry returns a storage.Entry value, ignoring the Stat info.
func newSubfieldEntry(v interface{}) *storage.Entry {
	return &storage.Entry{Value: deepcopy(v)}
}

// setRefs sets the cell's refs field.
func (c *Cell) setRefs() {
	r := refs.NewBuilder()
	r.AddValue(c.Value)
	r.AddDir(c.Dir)
	r.AddTags(c.Tags)
	c.refs = r.Get()
}

// get the *storage.Entry for a cell.
func (c *Cell) getEntry() *storage.Entry {
	entry := newSubfieldEntry(c.Value)
	c.fillStat(&entry.Stat)
	return entry
}

// get the *storage.Stat for a cell.
func (c *Cell) getStat() *storage.Stat {
	var stat storage.Stat
	c.fillStat(&stat)
	return &stat
}

// fillStat fills the storage.Stat struct with the the cell's metadata.  Assumes
// stat is not nil.
func (c *Cell) fillStat(stat *storage.Stat) {
	stat.ID = c.ID
	// TODO(jyh): Fill in the missing fields
}
