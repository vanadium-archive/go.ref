package query

import (
	"path/filepath"
	"testing"

	"veyron/services/store/memstore/state"

	"veyron2/storage"
)

type nameOptions []string

type globTest struct {
	path     string
	pattern  string
	expected []nameOptions
}

var globTests = []globTest{
	{"", "mvps/...", []nameOptions{
		{"mvps"},
		{"mvps/Links/0"},
		{"mvps/Links/1"},
	}},
	{"", "players/...", []nameOptions{
		{"players"},
		{"players/alfred"},
		{"players/alice"},
		{"players/betty"},
		{"players/bob"},
	}},
	// Note(mattr): This test case shows that Glob does not return
	// subfield nodes.
	{"", "mvps/*", []nameOptions{}},
	{"", "mvps/Links/*", []nameOptions{
		{"mvps/Links/0"},
		{"mvps/Links/1"},
	}},
	{"", "players/alfred", []nameOptions{
		{"players/alfred"},
	}},
	{"", "mvps/Links/0", []nameOptions{
		{"mvps/Links/0"},
	}},
	// An empty pattern returns the element referred to by the path.
	{"/mvps/Links/0", "", []nameOptions{
		{""},
	}},
	{"mvps", "Links/*", []nameOptions{
		{"Links/0"},
		{"Links/1"},
	}},
	{"mvps/Links", "*", []nameOptions{
		{"0"},
		{"1"},
	}},
}

// Test that an iterator doesen't get stuck in cycles.
func TestGlob(t *testing.T) {
	st := state.New(rootPublicID)
	sn := st.MutableSnapshot()

	type dir struct {
		Links []storage.ID
	}

	// Add some objects
	put(t, sn, "/", "")
	put(t, sn, "/teams", "")
	put(t, sn, "/teams/cardinals", "")
	put(t, sn, "/teams/sharks", "")
	put(t, sn, "/teams/bears", "")
	put(t, sn, "/players", "")
	alfredID := put(t, sn, "/players/alfred", "")
	put(t, sn, "/players/alice", "")
	bettyID := put(t, sn, "/players/betty", "")
	put(t, sn, "/players/bob", "")

	put(t, sn, "/mvps", &dir{[]storage.ID{alfredID, bettyID}})

	commit(t, st, sn)

	// Test that patterns starting with '/' are errors.
	_, err := Glob(st.Snapshot(), rootPublicID, storage.PathName{}, "/*")
	if err != filepath.ErrBadPattern {
		t.Errorf("Expected bad pattern error, got %v", err)
	}

	for _, gt := range globTests {
		path := storage.ParsePath(gt.path)
		it, err := Glob(st.Snapshot(), rootPublicID, path, gt.pattern)
		if err != nil {
			t.Errorf("Unexpected error on Glob: %s", err)
		}
		names := map[string]bool{}
		for ; it.IsValid(); it.Next() {
			names[it.Name()] = true
		}
		if len(names) != len(gt.expected) {
			t.Errorf("Wrong number of names for %s.  got %v, wanted %v",
				gt.pattern, names, gt.expected)
		}
		for _, options := range gt.expected {
			found := false
			for _, name := range options {
				if names[name] {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected to find one of %v in %v", options, names)
			}
		}
	}
}
