package field_test

import (
	"reflect"
	"testing"

	_ "veyron/lib/testutil"
	"veyron/services/store/memstore/field"

	"veyron2/storage"
)

type A struct {
	B int
	C []int
	D map[string]int

	E storage.ID
	F []storage.ID
	G map[string]storage.ID
}

type V struct {
	UID storage.ID
}

func pathEq(p1, p2 storage.PathName) bool {
	if len(p1) != len(p2) {
		return false
	}
	for i, x := range p1 {
		if p2[i] != x {
			return false
		}
	}
	return true
}

func TestGetField(t *testing.T) {
	v := &A{
		B: 5,
		C: []int{6, 7},
		D: map[string]int{"a": 8, "b": 9},
		E: storage.NewID(),
		F: []storage.ID{storage.NewID()},
		G: map[string]storage.ID{"a": storage.NewID()},
	}

	// Identity.
	x, s := field.Get(v, storage.PathName{})
	if x.Interface() != v || !pathEq(storage.PathName{}, s) {
		t.Errorf("Identity failed: %v", s)
	}

	// B field.
	x, s = field.Get(v, storage.PathName{"B"})
	if x.Interface() != 5 || !pathEq(storage.PathName{}, s) {
		t.Errorf("Expected 5, got %v, suffix=%v", x.Interface(), s)
	}

	// C field.
	x, s = field.Get(v, storage.PathName{"C"})
	if !pathEq(storage.PathName{}, s) {
		t.Errorf("Failed to get C: %v")
	}
	{
		y, ok := x.Interface().([]int)
		if !ok || len(y) != 2 || y[0] != 6 || y[1] != 7 {
			t.Errorf("C has the wrong value: %v", x)
		}
	}
	x, s = field.Get(v, storage.PathName{"C", "0"})
	if x.Interface() != 6 || !pathEq(storage.PathName{}, s) {
		t.Errorf("Expected 6, got %v, %v", x, s)
	}
	x, s = field.Get(v, storage.PathName{"C", "1"})
	if x.Interface() != 7 || !pathEq(storage.PathName{}, s) {
		t.Errorf("Expected 7, got %v, %v", x, s)
	}
	x, s = field.Get(v, storage.PathName{"C", "2"})
	if !pathEq(storage.PathName{"2"}, s) {
		t.Errorf("Expected %v, got %v", storage.PathName{"2"}, s)
	}
	{
		y, ok := x.Interface().([]int)
		if !ok || len(y) != 2 || y[0] != 6 || y[1] != 7 {
			t.Errorf("C has the wrong value: %v", x)
		}
	}

	// D field.
	x, s = field.Get(v, storage.PathName{"D"})
	if !pathEq(storage.PathName{}, s) {
		t.Errorf("Failed to get D")
	}
	{
		y, ok := x.Interface().(map[string]int)
		if !ok || len(y) != 2 || y["a"] != 8 || y["b"] != 9 {
			t.Errorf("Bad value: %v", y)
		}
	}
	x, s = field.Get(v, storage.PathName{"D", "a"})
	if x.Interface() != 8 || !pathEq(storage.PathName{}, s) {
		t.Errorf("Expected 8, got %v", x)
	}
	x, s = field.Get(v, storage.PathName{"D", "b"})
	if x.Interface() != 9 || !pathEq(storage.PathName{}, s) {
		t.Errorf("Expected 9, got %v", x)
	}
	x, s = field.Get(v, storage.PathName{"D", "c"})
	if !pathEq(storage.PathName{"c"}, s) {
		t.Errorf("Expected %v, got %v", storage.PathName{"c"}, s)
	}
	{
		y, ok := x.Interface().(map[string]int)
		if !ok || len(y) != 2 || y["a"] != 8 || y["b"] != 9 {
			t.Errorf("Bad value: %v", y)
		}
	}

	// E field.
	x, s = field.Get(v, storage.PathName{"E"})
	if x.Interface() != v.E || !pathEq(storage.PathName{}, s) {
		t.Errorf("Failed to get E: %v", x.Interface())
	}
	x, s = field.Get(v, storage.PathName{"E", "a", "b", "c"})
	if x.Interface() != v.E || !pathEq(storage.PathName{"a", "b", "c"}, s) {
		t.Errorf("Failed to get E: %v, %v", x.Interface(), s)
	}
}

func TestSetSliceField(t *testing.T) {
	v := &[]string{"a", "b", "c"}
	rv := reflect.ValueOf(v)

	// Test simple get and set.
	b, _ := field.Get(v, storage.PathName{"1"})
	if "b" != b.Interface() {
		t.Errorf(`Expected "b", got %v`, b.Interface())
	}
	if ok, _ := field.Set(rv, "1", "other"); ok != field.SetAsValue {
		t.Errorf("field.Set failed on slice: %v", ok)
	}
	b, _ = field.Get(v, storage.PathName{"1"})
	if "other" != b.Interface() {
		t.Errorf(`Expected "a", got %v`, b.Interface())
	}

	// Test get on a non-existant field.
	ne, _ := field.Get(v, storage.PathName{"4"})
	if ne.Kind() != reflect.Slice {
		t.Errorf("Expected to get a top level slice, got: %v", ne.Interface())
	}
	ne, _ = field.Get(v, storage.PathName{"-1"})
	if ne.Kind() != reflect.Slice {
		t.Errorf("Expected to get a top level slice, got: %v", ne.Interface())
	}
	nepath := storage.PathName{"X"}
	ne, s := field.Get(v, nepath)
	if ne.Kind() != reflect.Slice {
		t.Errorf("Expected to get a top level slice, got: %v", ne.Interface())
	}
	if !reflect.DeepEqual(s, nepath) {
		t.Errorf("Expected path %v, got %v.", nepath, s)
	}

	// Test adding a value.
	if ok, _ := field.Set(rv, "@", "AppendedVal"); ok != field.SetAsValue {
		t.Errorf("Expected to succeed in appending value: %v", ok)
	}
	appended, _ := field.Get(v, storage.PathName{"3"})
	if "AppendedVal" != appended.Interface() {
		t.Errorf(`Expected "AppendedVal", got %v`, appended.Interface())
	}

	// Test set of an incompatible value fails.
	if ok, _ := field.Set(rv, "1", true); ok == field.SetAsValue {
		t.Errorf("Expected field.Set to fail when an incompatible value is being set.")
	}
}

func TestSetEmptySliceField(t *testing.T) {
	v := &[]string{}
	rv := reflect.ValueOf(v)

	ne, _ := field.Get(v, storage.PathName{"0"})
	if ne.Kind() != reflect.Slice {
		t.Errorf("Expected to get a top level slice, got: %v", ne.Interface())
	}
	if ok, _ := field.Set(rv, "0", "a"); ok == field.SetAsValue {
		t.Errorf("Expected field.Set to fail")
	}
}

func TestSetMapField(t *testing.T) {
	v := &map[string]string{
		"A": "a",
		"B": "b",
	}
	rv := reflect.ValueOf(v)

	// Test simple get and set.
	a, _ := field.Get(v, storage.PathName{"A"})
	if "a" != a.Interface() {
		t.Errorf(`Expected "a", got %v`, a.Interface())
	}
	if ok, _ := field.Set(rv, "A", "other"); ok != field.SetAsValue {
		t.Errorf("field.Set failed on map: %v", ok)
	}
	a, _ = field.Get(v, storage.PathName{"A"})
	if "other" != a.Interface() {
		t.Errorf(`Expected "a", got %v`, a.Interface())
	}

	// Test get on a non-existant field.
	nepath := storage.PathName{"NonExistant"}
	ne, s := field.Get(v, nepath)
	if !reflect.DeepEqual(s, nepath) {
		t.Errorf("Expected path %v, got %v.", nepath, s)
	}
	if ne.Kind() != reflect.Map {
		t.Errorf("Expected to get a top level map, got: %v", ne.Interface())
	}

	// Test that set on a non-existant field adds the field.
	if ok, _ := field.Set(rv, "C", "c"); ok != field.SetAsValue {
		t.Errorf("Expected field.Set to succeed: %v", ok)
	}
	c, _ := field.Get(v, storage.PathName{"C"})
	if "c" != c.Interface() {
		t.Errorf(`Expected "c", got %v`, c.Interface())
	}

	// Test set of an incompatible value fails.
	if ok, _ := field.Set(rv, "A", true); ok == field.SetAsValue {
		t.Errorf("Expected field.Set to fail when an incompatible value is being set.")
	}
}

func TestSetEmptyMapField(t *testing.T) {
	v := &map[string]interface{}{}
	rv := reflect.ValueOf(v)

	ne, _ := field.Get(v, storage.PathName{"A"})
	if ne.Kind() != reflect.Map {
		t.Errorf("Expected to get a top level map, got: %v", ne.Interface())
	}
	if ok, _ := field.Set(rv, "A", "a"); ok != field.SetAsValue {
		t.Errorf("Expected field.Set to succeed: %v", ok)
	}
	a, _ := field.Get(v, storage.PathName{"A"})
	if "a" != a.Interface() {
		t.Errorf(`Expected "a", got %v`, a.Interface())
	}
}

func TestSetStructField(t *testing.T) {
	a := &A{
		B: 5,
		C: []int{6, 7},
		D: map[string]int{"a": 8, "b": 9},
		E: storage.NewID(),
		F: []storage.ID{storage.NewID()},
		G: map[string]storage.ID{"a": storage.NewID()},
	}
	v := reflect.ValueOf(a)

	// B field.
	x, _ := field.Get(a, storage.PathName{"B"})
	if x.Interface() != 5 {
		t.Errorf("Expected 5, got %v", x)
	}
	if ok, _ := field.Set(v, "B", 15); ok != field.SetAsValue {
		t.Errorf("field.Set failed: %v", ok)
	}
	x, _ = field.Get(a, storage.PathName{"B"})
	if x.Interface() != 15 {
		t.Errorf("Expected 15, got %v", x)
	}

	// C field.
	if ok, _ := field.Set(v, "C", []int{7}); ok != field.SetAsValue {
		t.Errorf("Failed to set C: %v", ok)
	}
	x, _ = field.Get(a, storage.PathName{"C", "0"})
	if x.Interface() != 7 {
		t.Errorf("Expected 6, got %v", x)
	}

	p, _ := field.Get(a, storage.PathName{"C"})
	if ok, _ := field.Set(p, "0", 8); ok != field.SetAsValue {
		t.Errorf("Failed to set C: %v", ok)
	}
	x, _ = field.Get(a, storage.PathName{"C", "0"})
	if x.Interface() != 8 {
		t.Errorf("Expected 8, got %v", x)
	}

	p, _ = field.Get(a, storage.PathName{"C"})
	if ok, _ := field.Set(p, "@", 9); ok != field.SetAsValue {
		t.Errorf("Failed to set C")
	}
	x, _ = field.Get(a, storage.PathName{"C", "1"})
	if x.Interface() != 9 {
		t.Errorf("Expected 9, got %v", x)
	}

	// D field.
	if ok, _ := field.Set(v, "D", map[string]int{"a": 1}); ok != field.SetAsValue {
		t.Errorf("Failed to set D")
	}
	x, _ = field.Get(a, storage.PathName{"D", "a"})
	if x.Interface() != 1 {
		t.Errorf("Expected 1, got %v", x)
	}

	p, _ = field.Get(a, storage.PathName{"D"})
	if ok, _ := field.Set(p, "a", 2); ok != field.SetAsValue {
		t.Errorf("Failed to set D")
	}
	x, _ = field.Get(a, storage.PathName{"D", "a"})
	if x.Interface() != 2 {
		t.Errorf("Expected 2, got %v", x)
	}

	// E field.
	id := storage.NewID()
	ok, id2 := field.Set(v, "E", id)
	if ok != field.SetAsValue || id2 != id {
		t.Errorf("Failed to set E: %b, %s/%s", ok, id, id2)
	}

	// F field.
	p, _ = field.Get(a, storage.PathName{"F"})
	ok, fid := field.Set(p, "0", "fail")
	if ok != field.SetAsID {
		t.Errorf("Failed to set F: %v", x)
	}
	x, _ = field.Get(a, storage.PathName{"F", "0"})
	if x.Interface() != fid {
		t.Errorf("Expected %v, got %v", id, x.Interface())
	}

	// G field.
	p, _ = field.Get(a, storage.PathName{"G"})
	ok, fid = field.Set(p, "key", "fail")
	if ok != field.SetAsID {
		t.Errorf("Failed to set G")
	}
	x, _ = field.Get(a, storage.PathName{"G", "key"})
	if x.Interface() != fid {
		t.Errorf("Expected %v, got %v", id, x)
	}
}

func TestRemoveField(t *testing.T) {
	a := &A{
		B: 5,
		C: []int{6, 7},
		D: map[string]int{"a": 8, "b": 9},
		E: storage.NewID(),
		F: []storage.ID{storage.NewID()},
		G: map[string]storage.ID{"a": storage.NewID()},
	}
	v := reflect.ValueOf(a)

	if field.Remove(v, "B") {
		t.Errorf("Unexpected success")
	}
	p, _ := field.Get(a, storage.PathName{"C"})
	if field.Remove(p, "0") {
		t.Errorf("Unexpected success")
	}
	p, _ = field.Get(a, storage.PathName{"D"})
	if !field.Remove(p, "a") {
		t.Errorf("Unexpected failure")
	}
	x, s := field.Get(a, storage.PathName{"D", "a"})
	if !pathEq(storage.PathName{"a"}, s) {
		t.Errorf("Unexpected value: %v", x)
	}
}
