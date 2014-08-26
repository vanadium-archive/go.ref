package refs

import (
	"reflect"
	"strconv"

	"veyron/runtimes/google/lib/functional"
	"veyron/services/store/raw"

	"veyron2/storage"
)

// Builder is used to collect all the references in a value, using
// reflection for traversal.
type Builder struct {
	refs Set
}

var (
	tyID     = reflect.TypeOf(storage.ID{})
	tyString = reflect.TypeOf("")
)

// NewBuilder constructs a new refs builder.
func NewBuilder() *Builder {
	return &Builder{refs: Empty}
}

// Get returns the references.
func (b *Builder) Get() Set {
	return b.refs
}

// AddDir adds the references contained in the directory.
func (b *Builder) AddDir(d functional.Set) {
	if d == nil {
		return
	}
	d.Iter(func(it interface{}) bool {
		b.refs = b.refs.Put(it.(*Ref))
		return true
	})
}

// AddDEntries adds the references contained in the DEntry list.
func (b *Builder) AddDEntries(d []*raw.DEntry) {
	for _, de := range d {
		b.refs = b.refs.Put(&Ref{ID: de.ID, Path: NewSingletonPath(de.Name)})
	}
}

// AddValue adds the references contained in the value, using reflection
// to traverse it.
func (b *Builder) AddValue(v interface{}) {
	if v == nil {
		return
	}
	b.addRefs(nil, reflect.ValueOf(v))
}

func (b *Builder) addRefs(path *Path, v reflect.Value) {
	if !v.IsValid() {
		return
	}
	ty := v.Type()
	if ty == tyID {
		b.refs = b.refs.Put(&Ref{ID: v.Interface().(storage.ID), Path: path})
		return
	}
	switch ty.Kind() {
	case reflect.Map:
		b.addMapRefs(path, v)
	case reflect.Array, reflect.Slice:
		b.addSliceRefs(path, v)
	case reflect.Interface, reflect.Ptr:
		b.addRefs(path, v.Elem())
	case reflect.Struct:
		b.addStructRefs(path, v)
	}
}

func (b *Builder) addMapRefs(path *Path, v reflect.Value) {
	for _, key := range v.MapKeys() {
		if key.Type().Kind() == reflect.String {
			name := key.Convert(tyString).Interface().(string)
			b.addRefs(path.Append(name), v.MapIndex(key))
		}
	}
}

func (b *Builder) addSliceRefs(path *Path, v reflect.Value) {
	l := v.Len()
	for i := 0; i < l; i++ {
		b.addRefs(path.Append(strconv.Itoa(i)), v.Index(i))
	}
}

func (b *Builder) addStructRefs(path *Path, v reflect.Value) {
	l := v.NumField()
	ty := v.Type()
	for i := 0; i < l; i++ {
		name := ty.Field(i).Name
		b.addRefs(path.Append(name), v.Field(i))
	}
}
