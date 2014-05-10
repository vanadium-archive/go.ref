package field

// reflect.go uses reflection to access the fields in a value.
//
//    getField(v, path)
//    setField(v, path, x)
//    removeField(v, path)
//
// The path is JSON-style, using field names.  For example, consider the
// following value.
//
//    type MyValue struct {
//       A int
//       B []int
//       C map[string]int
//       D map[string]struct{E int}
//    }
//
// Here are some possible paths:
//
//    var x MyValue = ...
//    getField(x, storage.PathName{"A"}) == x.A
//    getField(x, storage.PathName{"B", "7"}) == x.B[7]
//    getField(x, storage.PathName{"C", "a"}) == x.C["a"]
//    getField(x, storage.PathName{"D", "a", "E"}) == x.D["a"].E
//
//    setField(x, storage.PathName{"A"}, 17)
//    getField(x, storage.PathName{"A"}) == 17
//
//    setField(x, storage.PathName{"D", "a"}, struct{E: 12})
//    getField(x, storage.PathName{"D", "a", "E"}) == 12
//
//    removeField(x, storage.PathName{"D", "a"}
//    getField(x, storage.PathName{"D", "a", "E"}) fails

import (
	"reflect"
	"strconv"

	"veyron2/storage"
)

type SetResult uint32

const (
	SetFailed SetResult = iota
	SetAsValue
	SetAsID
)

var (
	nullID    storage.ID
	nullValue reflect.Value

	tyID = reflect.TypeOf(nullID)
)

// Get returns the value associated with a path, stopping at values that
// can't be resolved.  It returns the value at the maximum prefix of the path
// that can be resolved, and any suffix that remains.
func Get(val interface{}, path storage.PathName) (reflect.Value, storage.PathName) {
	v, suffix := findField(reflect.ValueOf(val), path)
	return v, path[suffix:]
}

// findField returns the field specified by the path, using reflection to
// traverse the value.  Returns the field, and how many components of the path
// were resolved.
func findField(v reflect.Value, path storage.PathName) (reflect.Value, int) {
	for i, field := range path {
		v1 := followPointers(v)
		if !v1.IsValid() {
			return v, i
		}
		v2 := findNextField(v1, field)
		if !v2.IsValid() {
			return v1, i
		}
		v = v2
	}
	return v, len(path)
}

func followPointers(v reflect.Value) reflect.Value {
	if !v.IsValid() {
		return v
	}
	kind := v.Type().Kind()
	for kind == reflect.Ptr || kind == reflect.Interface {
		v = v.Elem()
		if !v.IsValid() {
			return v
		}
		kind = v.Type().Kind()
	}
	return v
}

func findNextField(v reflect.Value, field string) reflect.Value {
	switch v.Type().Kind() {
	case reflect.Array, reflect.Slice:
		return findSliceField(v, field)
	case reflect.Map:
		return findMapField(v, field)
	case reflect.Struct:
		return v.FieldByName(field)
	default:
		return reflect.Value{}
	}
}

func findSliceField(v reflect.Value, field string) reflect.Value {
	l := v.Len()
	i, err := strconv.Atoi(field)
	if err != nil || i < 0 || i >= l {
		return reflect.Value{}
	}
	return v.Index(i)
}

func findMapField(v reflect.Value, field string) reflect.Value {
	tyKey := v.Type().Key()
	if v.IsNil() || tyKey.Kind() != reflect.String {
		return reflect.Value{}
	}
	return v.MapIndex(reflect.ValueOf(field).Convert(tyKey))
}

// Set assigns the value associated with a subfield of an object.
// If the field has type storage.ID, then id is stored instead of xval.
//
// Here are the possible cases:
//
// 1. setFieldFailed if the operation failed because the value has the wrong type
//    or the path doesn't exist.
//
// 2. setFieldAsValue if the operation was successful, and the value xval was
//    stored.  The returned storage.ID is null.
//
// 3. setFieldAsId if the operation was successful, but the type of the field is
//    storage.ID and xval does not have type storage.ID.  In this case, the value
//    xval is not stored; the storage.ID is returned instead.  If the field does
//    not already exist, a new storage.ID is created (and returned).
//
// The setFieldAsID case means that the value xval is to be stored as a separate
// value in the store, not as a subfield of the current value.
//
// As a special case, if the field type is storage.ID, and xval has type storage.ID,
// then it is case #2, setFieldAsValue.  The returned storage.ID is zero.
func Set(v reflect.Value, name string, xval interface{}) (SetResult, storage.ID) {
	v = followPointers(v)
	if !v.IsValid() {
		return SetFailed, nullID
	}
	switch v.Type().Kind() {
	case reflect.Map:
		return setMapField(v, name, xval)
	case reflect.Array, reflect.Slice:
		return setSliceField(v, name, xval)
	default:
		return setRegularField(v, name, xval)
	}
}

func setMapField(v reflect.Value, name string, xval interface{}) (SetResult, storage.ID) {
	tyV := v.Type()
	tyKey := tyV.Key()
	if tyKey.Kind() != reflect.String {
		return SetFailed, nullID
	}
	if v.IsNil() {
		v.Set(reflect.MakeMap(tyV))
	}
	key := reflect.ValueOf(name).Convert(tyKey)
	r, x, id := coerceValue(tyV.Elem(), v.MapIndex(key), xval)
	if r == SetFailed {
		return SetFailed, nullID
	}
	v.SetMapIndex(key, x)
	return r, id
}

func setSliceField(v reflect.Value, field string, xval interface{}) (SetResult, storage.ID) {
	if field == "@" {
		r, x, id := coerceValue(v.Type().Elem(), nullValue, xval)
		if r == SetFailed {
			return SetFailed, nullID
		}
		v.Set(reflect.Append(v, x))
		return r, id
	}
	l := v.Len()
	i, err := strconv.Atoi(field)
	if err != nil || i < 0 || i >= l {
		return SetFailed, nullID
	}
	r, x, id := coerceValue(v.Type().Elem(), v.Index(i), xval)
	if r == SetFailed {
		return SetFailed, nullID
	}
	v.Index(i).Set(x)
	return r, id
}

func setRegularField(v reflect.Value, name string, xval interface{}) (SetResult, storage.ID) {
	v = findNextField(v, name)
	if !v.CanSet() {
		return SetFailed, nullID
	}
	r, x, id := coerceValue(v.Type(), v, xval)
	if r == SetFailed {
		return SetFailed, nullID
	}
	v.Set(x)
	return r, id
}

func coerceValue(ty reflect.Type, prev reflect.Value, xval interface{}) (SetResult, reflect.Value, storage.ID) {
	x := reflect.ValueOf(xval)
	switch {
	case ty == tyID:
		if x.Type() == tyID {
			return SetAsValue, x, xval.(storage.ID)
		}
		var id storage.ID
		if prev.IsValid() {
			var ok bool
			if id, ok = prev.Interface().(storage.ID); !ok {
				return SetFailed, nullValue, nullID
			}
		} else {
			id = storage.NewID()
		}
		return SetAsID, reflect.ValueOf(id), id
	case x.Type().AssignableTo(ty):
		return SetAsValue, x, nullID
	case x.Type().ConvertibleTo(ty):
		return SetAsValue, x.Convert(ty), nullID
	default:
		return SetFailed, nullValue, nullID
	}
}

// Remove removes a field associated with a path.
// Return the old value and true iff the update succeeded.
func Remove(v reflect.Value, name string) bool {
	v = followPointers(v)
	if !v.IsValid() || v.Type().Kind() != reflect.Map || v.IsNil() {
		return false
	}
	return removeMapField(v, name)
}

func removeMapField(v reflect.Value, name string) bool {
	// TODO(jyh): Also handle cases where field is a primitive scalar type like
	// int or bool.
	tyKey := v.Type().Key()
	if tyKey.Kind() != reflect.String {
		return false
	}
	v.SetMapIndex(reflect.ValueOf(name).Convert(tyKey), reflect.Value{})
	return true
}
