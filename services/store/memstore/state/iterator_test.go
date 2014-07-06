package state_test

import (
	"runtime"
	"testing"

	"veyron/services/store/memstore/refs"
	"veyron/services/store/memstore/state"
	"veyron2/security"
	"veyron2/storage"
)

// check that the iterator produces a set of names.
func checkAcyclicIterator(t *testing.T, sn *state.MutableSnapshot, id security.PublicID, filter state.IterFilter, names []string) {
	_, file, line, _ := runtime.Caller(1)

	// Construct an index of names.
	index := map[string]bool{}
	for _, name := range names {
		index[name] = false
	}

	// Compute the found names.
	for it := sn.NewIterator(id, storage.ParsePath("/"), state.ListPaths, filter); it.IsValid(); it.Next() {
		name := it.Name()
		if found, ok := index[name]; ok {
			if found {
				t.Errorf("%s(%d): duplicate name %q", file, line, name)
			}
			index[name] = true
		} else {
			t.Errorf("%s(%d): unexpected name %q", file, line, name)
		}
	}

	// Print the not found names.
	for name, found := range index {
		if !found {
			t.Errorf("%s(%d): expected: %v", file, line, name)
		}
	}
}

// check that the iterator produces a set of names.  Since entries in the store
// can have multiple names, the names are provided using a set of equivalence
// classes.  The requirement is that the iterator produces exactly one name from
// each equivalence class. Order doesn't matter.
func checkUniqueObjectsIterator(t *testing.T, sn *state.MutableSnapshot, id security.PublicID, filter state.IterFilter, names [][]string) {
	_, file, line, _ := runtime.Caller(1)

	// Construct an index of name to equivalence class.
	index := map[string]int{}
	for i, equiv := range names {
		for _, name := range equiv {
			index[name] = i
		}
	}

	// Compute the found set of equivalence classes.
	found := map[int]bool{}
	for it := sn.NewIterator(id, storage.ParsePath("/"), state.ListObjects, filter); it.IsValid(); it.Next() {
		name := it.Name()
		if i, ok := index[name]; ok {
			if _, ok := found[i]; ok {
				t.Errorf("%s(%d): duplicate name %q", file, line, name)
			}
			found[i] = true
		} else {
			t.Errorf("%s(%d): unexpected name %q", file, line, name)
		}
	}

	// Print the not found equivalence classes.
	for i, equiv := range names {
		if !found[i] {
			t.Errorf("%s(%d): expected one of: %v", file, line, equiv)
		}
	}
}

// Tests that an iterator returns all non-cyclic paths that reach an object.
func TestDuplicatePaths(t *testing.T) {
	st := state.New(rootPublicID)
	sn := st.MutableSnapshot()

	// Add some objects
	put(t, sn, rootPublicID, "/", "")
	put(t, sn, rootPublicID, "/teams", "")
	put(t, sn, rootPublicID, "/teams/cardinals", "")
	put(t, sn, rootPublicID, "/players", "")
	mattID := put(t, sn, rootPublicID, "/players/matt", "")

	// Add some hard links
	put(t, sn, rootPublicID, "/teams/cardinals/mvp", mattID)

	checkAcyclicIterator(t, sn, rootPublicID, nil, []string{
		"",
		"teams",
		"players",
		"teams/cardinals",
		"players/matt",
		"teams/cardinals/mvp",
	})
	checkUniqueObjectsIterator(t, sn, rootPublicID, nil, [][]string{
		{""},
		{"teams"},
		{"players"},
		{"teams/cardinals"},
		{"players/matt", "teams/cardinals/mvp"},
	})

	// Test that the iterator does not revisit objects on previously rejected paths.
	rejected := false
	rejectMatt := func(fullPath *refs.FullPath, path *refs.Path) (bool, bool) {
		name := fullPath.Append(path.Suffix(1)).Name().String()
		if !rejected && (name == "players/matt" || name == "teams/cardinals/mvp") {
			rejected = true
			return false, true
		}
		return true, true
	}
	checkUniqueObjectsIterator(t, sn, rootPublicID, rejectMatt, [][]string{
		{""},
		{"teams"},
		{"players"},
		{"teams/cardinals"},
	})
}

// Test that an iterator doesn't get stuck in cycles.
func TestCyclicStructure(t *testing.T) {
	st := state.New(rootPublicID)
	sn := st.MutableSnapshot()

	// Add some objects
	put(t, sn, rootPublicID, "/", "")
	put(t, sn, rootPublicID, "/teams", "")
	cardinalsID := put(t, sn, rootPublicID, "/teams/cardinals", "")
	put(t, sn, rootPublicID, "/players", "")
	mattID := put(t, sn, rootPublicID, "/players/matt", "")
	put(t, sn, rootPublicID, "/players/joe", "")

	// Add some hard links
	put(t, sn, rootPublicID, "/players/matt/team", cardinalsID)
	put(t, sn, rootPublicID, "/teams/cardinals/mvp", mattID)

	checkAcyclicIterator(t, sn, rootPublicID, nil, []string{
		"",
		"teams",
		"players",
		"players/joe",
		"players/matt",
		"teams/cardinals/mvp",
		"teams/cardinals",
		"players/matt/team",
	})
	checkUniqueObjectsIterator(t, sn, rootPublicID, nil, [][]string{
		{""},
		{"teams"},
		{"players"},
		{"players/joe"},
		{"players/matt", "teams/cardinals/mvp"},
		{"teams/cardinals", "players/matt/team"},
	})
}

func TestIteratorSecurity(t *testing.T) {
	st := state.New(rootPublicID)
	sn := st.MutableSnapshot()

	// Create /Users/jane and give her RWA permissions.
	janeACLID := putPath(t, sn, rootPublicID, "/Users/jane/acls/janeRWA", &storage.ACL{
		Name: "Jane",
		Contents: security.ACL{
			janeUser: security.LabelSet(security.ReadLabel | security.WriteLabel | security.AdminLabel),
		},
	})
	janeTags := storage.TagList{
		storage.Tag{Op: storage.AddInheritedACL, ACL: janeACLID},
		storage.Tag{Op: storage.RemoveACL, ACL: state.EveryoneACLID},
	}
	put(t, sn, rootPublicID, "/Users/jane/.tags", janeTags)
	put(t, sn, rootPublicID, "/Users/jane/aaa", "stuff")
	sharedID := put(t, sn, rootPublicID, "/Users/jane/shared", "stuff")

	// Create /Users/john and give him RWA permissions.
	johnACLID := putPath(t, sn, rootPublicID, "/Users/john/acls/johnRWA", &storage.ACL{
		Name: "John",
		Contents: security.ACL{
			johnUser: security.LabelSet(security.ReadLabel | security.WriteLabel | security.AdminLabel),
		},
	})
	johnTags := storage.TagList{
		storage.Tag{Op: storage.AddInheritedACL, ACL: johnACLID},
		storage.Tag{Op: storage.RemoveACL, ACL: state.EveryoneACLID},
	}
	put(t, sn, rootPublicID, "/Users/john/.tags", johnTags)
	put(t, sn, rootPublicID, "/Users/john/aaa", "stuff")
	put(t, sn, rootPublicID, "/Users/john/shared", sharedID)

	// Root gets everything.
	checkAcyclicIterator(t, sn, rootPublicID, nil, []string{
		"",
		"Users",
		"Users/jane",
		"Users/jane/acls",
		"Users/jane/acls/janeRWA",
		"Users/jane/aaa",
		"Users/john",
		"Users/john/acls",
		"Users/john/acls/johnRWA",
		"Users/john/aaa",
		"Users/jane/shared",
		"Users/john/shared",
	})
	checkUniqueObjectsIterator(t, sn, rootPublicID, nil, [][]string{
		{""},
		{"Users"},
		{"Users/jane"},
		{"Users/jane/acls"},
		{"Users/jane/acls/janeRWA"},
		{"Users/jane/aaa"},
		{"Users/john"},
		{"Users/john/acls"},
		{"Users/john/acls/johnRWA"},
		{"Users/john/aaa"},
		{"Users/jane/shared", "Users/john/shared"},
	})

	// Jane sees only her names.
	checkAcyclicIterator(t, sn, janePublicID, nil, []string{
		"",
		"Users",
		"Users/jane",
		"Users/jane/acls",
		"Users/jane/acls/janeRWA",
		"Users/jane/aaa",
		"Users/jane/shared",
	})
	checkUniqueObjectsIterator(t, sn, janePublicID, nil, [][]string{
		{""},
		{"Users"},
		{"Users/jane"},
		{"Users/jane/acls"},
		{"Users/jane/acls/janeRWA"},
		{"Users/jane/aaa"},
		{"Users/jane/shared"},
	})

	// John sees only his names.
	checkAcyclicIterator(t, sn, johnPublicID, nil, []string{
		"",
		"Users",
		"Users/john",
		"Users/john/acls",
		"Users/john/acls/johnRWA",
		"Users/john/aaa",
		"Users/john/shared",
	})
	checkUniqueObjectsIterator(t, sn, johnPublicID, nil, [][]string{
		{""},
		{"Users"},
		{"Users/john"},
		{"Users/john/acls"},
		{"Users/john/acls/johnRWA"},
		{"Users/john/aaa"},
		{"Users/john/shared"},
	})
}
