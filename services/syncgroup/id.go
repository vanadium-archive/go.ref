package syncgroup

import "veyron2/storage"

// The current implementation of SyncGroupID assumes that an ID is a storage.ID.
// ID is defined in syncgroup.vdl.

// NewID returns a new ID.
func NewID() ID {
	return ID(storage.NewID())
}

// String returns id as a hex string.
func (id ID) String() string {
	return storage.ID(id).String()
}

// TODO(m3b): Add a string-to-ID function here.  Perhaps it should conform to fmt.Scanner,
// to parallel String() conforming to fmt.Stringer.

// IsValid returns whether id is not the zero id.
func (id ID) IsValid() bool {
	return storage.ID(id).IsValid()
}

// CompareIDs compares two IDs lexicographically.
func CompareIDs(id1 ID, id2 ID) int {
	return storage.CompareIDs(storage.ID(id1), storage.ID(id2))
}
