package state

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"

	"veyron2/storage"
)

// This function returns an interface with a pointer to the element in val.
// Note that this is different from returning &val which returns a pointer to
// val. This is required temporarily before we switch to VOM value so that
// we can append to slices.
// TODO(bprosnitz) This is hacky -- remove this when we switch to VOM Value
func makeInnerReference(val interface{}) interface{} {
	rv := reflect.ValueOf(val)
	ptr := reflect.New(rv.Type())
	ptr.Elem().Set(rv)
	return ptr.Interface()
}

func deepcopyReflect(rv reflect.Value) reflect.Value {
	switch rv.Kind() {
	case reflect.Array:
		arr := reflect.New(rv.Type()).Elem()
		for i := 0; i < rv.Len(); i++ {
			valcopy := deepcopyReflect(rv.Index(i))
			arr.Index(i).Set(valcopy)
		}
		return arr
	case reflect.Slice:
		s := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Cap())
		for i := 0; i < rv.Len(); i++ {
			valcopy := deepcopyReflect(rv.Index(i))
			s.Index(i).Set(valcopy)
		}
		ptr := reflect.New(rv.Type())
		ptr.Elem().Set(s)
		return ptr.Elem()
	case reflect.Map:
		m := reflect.MakeMap(rv.Type())
		keys := rv.MapKeys()
		for _, key := range keys {
			val := rv.MapIndex(key)
			keycopy := deepcopyReflect(key)
			valcopy := deepcopyReflect(val)
			m.SetMapIndex(keycopy, valcopy)
		}
		return m
	case reflect.Struct:
		s := reflect.New(rv.Type()).Elem()
		for i := 0; i < rv.NumField(); i++ {
			valcopy := deepcopyReflect(rv.Field(i))
			s.Field(i).Set(valcopy)
		}
		return s
	case reflect.Ptr:
		ptr := reflect.New(rv.Type()).Elem()
		elem := reflect.New(rv.Type().Elem())
		ptr.Set(elem)
		ptr.Elem().Set(deepcopyReflect(rv.Elem()))
		return ptr
	case reflect.Interface:
		intr := reflect.New(rv.Type()).Elem()
		intr.Set(deepcopyReflect(rv.Elem()))
		return intr
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		panic(fmt.Sprintf("deepcopy of kind %v not supported", rv.Kind()))
	default:
		// Primitives (copy it so we can't set the original)
		return reflect.ValueOf(rv.Interface())
	}
}

// deepcopy performs a deep copy of a value.  We need this to simulate secondary
// storage where each time a value is stored, it is copied to secondary storage;
// and when it is retrieved, it is copied out of secondary storage.
func deepcopy(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	return deepcopyReflect(reflect.ValueOf(v)).Interface()
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
