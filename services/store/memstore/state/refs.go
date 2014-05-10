package state

import (
	"veyron/services/store/memstore/refs"

	"veyron2/storage"
)

// refsExist returns true iff there is a value in the state for each reference.
func (sn *snapshot) refsExist(ids refs.Set) bool {
	for it := ids.Iterator(); it.IsValid(); it.Next() {
		id := it.Get().(*refs.Ref).ID
		if !sn.idTable.Contains(&Cell{ID: id}) {
			return false
		}
	}
	return true
}

// updateRefs takes a set of references from <before> to <after>.  Any reference
// in (<before> - <after>) should be decremented, and any reference in (<after>
// - <before>) should be incremented.
func (sn *MutableSnapshot) updateRefs(id storage.ID, beforeRefs, afterRefs refs.Set) {
	it1 := beforeRefs.Iterator()
	it2 := afterRefs.Iterator()
	for it1.IsValid() && it2.IsValid() {
		r1 := it1.Get().(*refs.Ref)
		r2 := it2.Get().(*refs.Ref)
		cmp := storage.CompareIDs(r1.ID, r2.ID)
		switch {
		case cmp < 0:
			sn.removeRef(id, r1)
			it1.Next()
		case cmp > 0:
			sn.addRef(id, r2)
			it2.Next()
		case cmp == 0:
			it1.Next()
			it2.Next()
		}
	}
	for ; it1.IsValid(); it1.Next() {
		sn.removeRef(id, it1.Get().(*refs.Ref))
	}
	for ; it2.IsValid(); it2.Next() {
		sn.addRef(id, it2.Get().(*refs.Ref))
	}
}

func (sn *MutableSnapshot) addRefs(id storage.ID, r refs.Set) {
	r.Iter(func(it interface{}) bool {
		sn.addRef(id, it.(*refs.Ref))
		return true
	})
}

func (sn *MutableSnapshot) removeRefs(id storage.ID, r refs.Set) {
	r.Iter(func(it interface{}) bool {
		sn.removeRef(id, it.(*refs.Ref))
		return true
	})
}

func (sn *MutableSnapshot) addRef(id storage.ID, r *refs.Ref) {
	// Update refcount.
	sn.ref(r.ID)

	// Add the inverse link.
	c := sn.Find(r.ID)
	c.inRefs = c.inRefs.Put(&refs.Ref{ID: id, Path: r.Path, Label: r.Label})
}

func (sn *MutableSnapshot) removeRef(id storage.ID, r *refs.Ref) {
	// Remove the inverse link.
	c := sn.deref(r.ID)
	c.inRefs = c.inRefs.Remove(&refs.Ref{ID: id, Path: r.Path, Label: r.Label})

	// Update refcount.
	sn.unref(r.ID)
}
