// Modeled after veyron2/storage/vstore/blackbox/photoalbum_test.go.
//
// TODO(sadovsky): Maybe migrate this to be part of the public store API, to
// help with writing tests that use storage.

package state

import (
	"reflect"
	"runtime"
	"testing"

	"veyron2/security"
	"veyron2/storage"
)

var (
	rootPublicID security.PublicID = security.FakePublicID("root")
)

func get(t *testing.T, sn *MutableSnapshot, path string) interface{} {
	_, file, line, _ := runtime.Caller(1)
	name := storage.ParsePath(path)
	e, err := sn.Get(rootPublicID, name)
	if err != nil {
		t.Fatalf("%s(%d): can't get %s: %s", file, line, path, err)
	}
	return e.Value
}

func put(t *testing.T, sn *MutableSnapshot, path string, v interface{}) storage.ID {
	_, file, line, _ := runtime.Caller(1)
	name := storage.ParsePath(path)
	stat, err := sn.Put(rootPublicID, name, v)
	if err != nil {
		t.Errorf("%s(%d): can't put %s: %s", file, line, path, err)
	}
	if _, err := sn.Get(rootPublicID, name); err != nil {
		t.Errorf("%s(%d): can't get %s: %s", file, line, path, err)
	}
	if stat != nil {
		return stat.ID
	}
	return storage.ID{}
}

func remove(t *testing.T, sn *MutableSnapshot, path string) {
	name := storage.ParsePath(path)
	if err := sn.Remove(rootPublicID, name); err != nil {
		_, file, line, _ := runtime.Caller(1)
		t.Errorf("%s(%d): can't remove %s: %s", file, line, path, err)
	}
}

func TestDeepcopy(t *testing.T) {
	type basicTestStruct struct {
		X int
	}
	type compositeHoldingTestStruct struct {
		A  [2]int
		Sl []string
		M  map[string]int
		St basicTestStruct
	}

	var intr interface{} = []string{"X"}

	tests := []interface{}{
		nil,
		0,
		true,
		"str",
		[3]int{4, 5, 6},
		[]string{"A", "B"},
		map[string]int{"A": 4, "B": 3},
		basicTestStruct{7},
		&basicTestStruct{7},
		compositeHoldingTestStruct{
			[2]int{3, 4},
			[]string{"A"},
			map[string]int{"A": 5},
			basicTestStruct{X: 3},
		},
		intr,
	}

	for _, test := range tests {
		copiedVal := deepcopy(test)
		if !reflect.DeepEqual(copiedVal, test) {
			t.Errorf("failure in deepcopy. Expected %v, got %v", test, copiedVal)
		}
	}
}

func TestDeepcopySliceSettability(t *testing.T) {
	rvSliceCopy := deepcopyReflect(reflect.ValueOf([]int{3, 4}))
	if !rvSliceCopy.CanSet() {
		t.Errorf("can't set slice. This is required for appending to slices")
	}
}

func TestDeepcopyNilMap(t *testing.T) {
	var nilMap map[int]int
	mapCopy := deepcopy(nilMap)
	if !reflect.DeepEqual(mapCopy, map[int]int{}) {
		t.Errorf("expected an empty map, got %v", mapCopy)
	}

	type structWithMap struct {
		M map[int]int
	}
	s := deepcopy(&structWithMap{})
	if !reflect.DeepEqual(s, &structWithMap{map[int]int{}}) {
		t.Errorf("expected an empty map in the struct, got %v", s)
	}
}
