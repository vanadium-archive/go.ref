package syncgroup

import (
	"encoding/hex"
	"veyron2/storage"
	"veyron2/verror"
)

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

// ParseID returns an ID formed from the string str, which will normally have
// been created with ID.String().
func ParseID(str string) (id ID, err error) {
	var tmp []byte
	tmp, err = hex.DecodeString(str)
	if err != nil {
		return id, err
	}
	if len(tmp) == 0 || len(tmp) > len(id) {
		return id, verror.BadProtocolf("string \"%v\" has wrong length to be a syncgroup.ID", str)
	}
	for i := 0; i != len(tmp); i++ {
		id[i] = tmp[i]
	}
	return id, err
}

// IsValid returns whether id is not the zero id.
func (id ID) IsValid() bool {
	return storage.ID(id).IsValid()
}

// CompareIDs compares two IDs lexicographically.
func CompareIDs(id1 ID, id2 ID) int {
	return storage.CompareIDs(storage.ID(id1), storage.ID(id2))
}
