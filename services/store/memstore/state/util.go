package state

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"

	"veyron2/storage"
	"veyron2/vom"
)

// deepcopy performs a deep copy of a value.  We need this to simulate secondary
// storage where each time a value is stored, it is copied to secondary storage;
// and when it is retrieved, it is copied out of secondary storage.
func deepcopy(v interface{}) interface{} {
	var buf bytes.Buffer
	if err := vom.NewEncoder(&buf).Encode(v); err != nil {
		panic(fmt.Sprintf("Can't encode %v: %s", v, err))
	}
	cp := reflect.New(reflect.TypeOf(v)).Elem().Interface()
	if err := vom.NewDecoder(&buf).Decode(&cp); err != nil {
		panic(fmt.Sprintf("Can't decode %v: %s", v, err))
	}
	return cp
}

// addIDToSort adds a storage.ID to a sorted array.
func addIDToSort(id storage.ID, refs []storage.ID) []storage.ID {
	i := findIDInSort(id, refs)
	newRefs := make([]storage.ID, len(refs)+1)
	copy(newRefs[0:i], refs[0:i])
	newRefs[i] = id
	copy(newRefs[i+1:], refs[i:])
	return newRefs
}

// removeIDFromSort removes a storage.ID from a sorted array.
func removeIDFromSort(id storage.ID, refs []storage.ID) []storage.ID {
	i := findIDInSort(id, refs)
	if i < len(refs) && refs[i] == id {
		newRefs := make([]storage.ID, len(refs)-1)
		copy(newRefs[0:i], refs[0:i])
		copy(newRefs[i:], refs[i+1:])
		return newRefs
	}
	return refs
}

func findIDInSort(id storage.ID, refs []storage.ID) int {
	return sort.Search(len(refs), func(i int) bool {
		return bytes.Compare(refs[i][:], id[:]) >= 0
	})
}
