// Package state implements an in-memory version of the store state.
// There are three main types here.
//
// Snapshot represents an isolated read-only copy of the state.
// It supports only iteration.
//
//      NewIterator() : returns an iterator to traverse the state.
//
// MutableSnapshot is an isolated read-write copy of the state.  It supports the
// standard dictionary operations.
//
//      Read(path) : fetches a value from the snapshot.
//      Put(path, value) : stores a value in the snapshot.
//      Remove(path) : remove a value from the snapshot.
//      Mutations() : returns the set of mutations to the snapshot.
//
// State is the actual shared state, with the following methods.
//
//      Snapshot() : returns a read-only snapshot of the state.
//      MutableSnapshot() : returns a read-write snapshot of the state.
//      ApplyMutations(mutations) : applies the mutations to the state.
//
// ApplyMutations can fail due to concurrency where the state was modified
// (through another call to ApplyMutations) between the time that a
// MutableSnapshot created to the time that it is applied.
//
// The sequence for performing an atomic change is to copy the snapshot,
// make the changes on the copy, the apply the changes to the original state
// atomically.
//
//    // Swap /a/b/c and /d/e atomically.
//    st := New(...)
//    ...
//    sn := st.MutableSnapshot()
//    x, err := sn.Get("/a/b/c")
//    y, err := sn.Get("/d/e")
//    err = sn.Put("/d/e", x)
//    err = sn.Put("/a/b/c", y)
//    err = st.ApplyMutations(sn.Mutations())
package state
