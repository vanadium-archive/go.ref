package refs_test

import (
	"strconv"
	"testing"

	"veyron/services/store/memstore/refs"

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

func TestID(t *testing.T) {
	id := storage.NewID()

	b := refs.NewBuilder()
	b.AddValue(id)
	r := b.Get()
	if r.Len() != 1 {
		t.Errorf("Expected 1 element, got %d", r.Len())
	}
	r.Iter(func(it interface{}) bool {
		r := it.(*refs.Ref)
		if r.ID != id {
			t.Errorf("Expected %s, got %s", id, r.ID)
		}
		if r.Path != nil {
			t.Errorf("Expected nil, got %v", r.Path)
		}
		return true
	})
}

func TestArray(t *testing.T) {
	a := []storage.ID{storage.NewID(), storage.NewID()}

	b := refs.NewBuilder()
	b.AddValue(a)
	r := b.Get()
	if r.Len() != 2 {
		t.Errorf("Expected 2 elements, got %d", r.Len())
	}
	r.Iter(func(it interface{}) bool {
		r := it.(*refs.Ref)
		found := false
		for i, id := range a {
			if r.ID == id {
				p := refs.NewSingletonPath(strconv.Itoa(i))
				if r.Path != p {
					t.Errorf("Expected %s, got %s", p, r.Path)
				}
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Unexpected reference: %v", r)
		}
		return true
	})
}

func TestStruct(t *testing.T) {
	v := &A{
		B: 5,
		C: []int{6, 7},
		D: map[string]int{"a": 8, "b": 9},
		E: storage.NewID(),
		F: []storage.ID{storage.NewID()},
		G: map[string]storage.ID{"a": storage.NewID()},
	}
	paths := make(map[storage.ID]*refs.Path)
	paths[v.E] = refs.NewSingletonPath("E")
	paths[v.F[0]] = refs.NewSingletonPath("F").Append("0")
	paths[v.G["a"]] = refs.NewSingletonPath("G").Append("a")

	b := refs.NewBuilder()
	b.AddValue(v)
	r := b.Get()
	if r.Len() != 3 {
		t.Errorf("Expected 3 elements, got %d", r.Len())
	}
	r.Iter(func(it interface{}) bool {
		r := it.(*refs.Ref)
		p, ok := paths[r.ID]
		if !ok {
			t.Errorf("Unexpected id %s", r.ID)
		}
		if r.Path != p {
			t.Errorf("Expected %s, got %s", p, r.Path)
		}
		return true
	})
}
