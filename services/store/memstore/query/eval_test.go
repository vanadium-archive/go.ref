package query

import (
	"fmt"
	"reflect"
	"testing"
	"time"
	"veyron2/services/store"
	"veyron2/vdl"

	"veyron/services/store/memstore/state"

	"veyron2/query"
	"veyron2/security"
	"veyron2/storage"
)

type team struct {
	Name     string
	Location string
}

type player struct {
	Name string
	Age  int
}

func populate(t *testing.T) *state.State {
	st := state.New(rootPublicID)
	sn := st.MutableSnapshot()

	// Add some objects.
	put(t, sn, "/", "")

	put(t, sn, "/players", "")
	alfredID := put(t, sn, "/players/alfred", player{"alfred", 17})
	aliceID := put(t, sn, "/players/alice", player{"alice", 16})
	bettyID := put(t, sn, "/players/betty", player{"betty", 23})
	bobID := put(t, sn, "/players/bob", player{"bob", 21})

	put(t, sn, "/teams", "")
	put(t, sn, "/teams/cardinals", team{"cardinals", "CA"})
	put(t, sn, "/teams/sharks", team{"sharks", "NY"})
	put(t, sn, "/teams/bears", team{"bears", "CO"})

	put(t, sn, "/teams/cardinals/alfred", alfredID)
	put(t, sn, "/teams/sharks/alice", aliceID)
	put(t, sn, "/teams/sharks/betty", bettyID)
	put(t, sn, "/teams/sharks/bob", bobID)

	commit(t, st, sn)
	return st
}

func TestEval(t *testing.T) {
	st := populate(t)

	type testCase struct {
		suffix        string
		query         string
		expectedNames []string
	}

	tests := []testCase{
		// nameEvaluator:
		{"", "teams", []string{"teams"}},
		{"", "teams/.", []string{"teams"}},
		{"", "teams/*", []string{"teams", "teams/cardinals", "teams/sharks", "teams/bears"}},

		// With a non empty prefix:
		{"teams", ".", []string{""}},
		{"teams", "*", []string{"", "cardinals", "sharks", "bears"}},

		// typeEvaluator:
		{"", "teams | type team", []string{}},
		{"", "teams/. | type team", []string{}},
		{"", "teams/* | type team", []string{"teams/cardinals", "teams/sharks", "teams/bears"}},

		// filterEvaluator/predicateBool:
		{"", "teams | ?true", []string{"teams"}},
		{"", "teams | ?false", []string{}},

		// predicateCompare:
		// String constants:
		{"", "teams | ?'foo' > 'bar'", []string{"teams"}},
		{"", "teams | ?'foo' < 'bar'", []string{}},
		{"", "teams | ?'foo' == 'bar'", []string{}},
		{"", "teams | ?'foo' != 'bar'", []string{"teams"}},
		{"", "teams | ?'foo' <= 'bar'", []string{}},
		{"", "teams | ?'foo' >= 'bar'", []string{"teams"}},
		// Rational number constants:
		{"", "teams | ?2.3 > 1.0", []string{"teams"}},
		{"", "teams | ?2.3 < 1.0", []string{}},
		{"", "teams | ?2.3 == 1.0", []string{}},
		{"", "teams | ?2.3 != 1.0", []string{"teams"}},
		{"", "teams | ?2.3 <= 1.0", []string{}},
		{"", "teams | ?2.3 >= 1.0", []string{"teams"}},
		{"", "teams | ?-2.3 >= 1.0", []string{}},
		{"", "teams | ?2.3 <= -1.0", []string{}},
		// Integer constants:
		{"", "teams | ?2 > 1", []string{"teams"}},
		{"", "teams | ?2 < 1", []string{}},
		{"", "teams | ?2 == 1", []string{}},
		{"", "teams | ?2 != 1", []string{"teams"}},
		{"", "teams | ?2 <= 1", []string{}},
		{"", "teams | ?2 >= 1", []string{"teams"}},
		// Compare an integer with a rational number:
		{"", "teams | ?2 > 1.7", []string{"teams"}},
		{"", "teams | ?2.3 > 1", []string{"teams"}},
		{"", "teams | ?-2 > 1.7", []string{}},
		// Veyron names:
		{"", "teams/* | type team | ?Name > 'bar'", []string{"teams/cardinals", "teams/sharks", "teams/bears"}},
		{"", "teams/* | type team | ?Name > 'foo'", []string{"teams/sharks"}},
		{"", "teams/* | type team | ?Name != 'bears'", []string{"teams/cardinals", "teams/sharks"}},
		{"", "players/* | type player | ?Age > 20", []string{"players/betty", "players/bob"}},
		{"", "players/* | type player | ?-Age < -20", []string{"players/betty", "players/bob"}},

		// predicateAnd:
		{"", "teams | ?true && true", []string{"teams"}},
		{"", "teams | ?true && false", []string{}},

		// predicateOr:
		{"", "teams | ?true || true", []string{"teams"}},
		{"", "teams | ?true || false", []string{"teams"}},
		{"", "teams | ?false || false", []string{}},

		// predicateNot:
		{"", "teams | ?!true", []string{}},
		{"", "teams | ?!false", []string{"teams"}},
		{"", "teams | ?!(false && false)", []string{"teams"}},
		{"", "teams | ?!(true || false)", []string{}},
	}
	for _, test := range tests {
		it := Eval(st.Snapshot(), rootPublicID, storage.ParsePath(test.suffix), query.Query{test.query})
		names := map[string]bool{}
		for it.Next() {
			result := it.Get()
			names[result.Name] = true
		}
		if it.Err() != nil {
			t.Errorf("query: %s, Error during eval: %v", test.query, it.Err())
			continue
		}
		if len(names) != len(test.expectedNames) {
			t.Errorf("query: %s, Wrong number of names.  got %v, wanted %v", test.query, names, test.expectedNames)
			continue
		}
		for _, name := range test.expectedNames {
			if !names[name] {
				t.Errorf("Did not find '%s' in %v", name, names)
			}
		}
		// Ensure that all the goroutines are cleaned up.
		it.(*evalIterator).wait()
	}
}

func TestSorting(t *testing.T) {
	st := populate(t)
	sn := st.MutableSnapshot()
	put(t, sn, "/teams/beavers", team{"beavers", "CO"})
	commit(t, st, sn)

	type testCase struct {
		query           string
		expectedResults []*store.QueryResult
	}

	tests := []testCase{
		{
			"'teams/*' | type team | sort()",
			[]*store.QueryResult{
				&store.QueryResult{0, "teams/bears", nil, team{"bears", "CO"}},
				&store.QueryResult{0, "teams/beavers", nil, team{"beavers", "CO"}},
				&store.QueryResult{0, "teams/cardinals", nil, team{"cardinals", "CA"}},
				&store.QueryResult{0, "teams/sharks", nil, team{"sharks", "NY"}},
			},
		},
		{
			"'teams/*' | type team | sort(Name)",
			[]*store.QueryResult{
				&store.QueryResult{0, "teams/bears", nil, team{"bears", "CO"}},
				&store.QueryResult{0, "teams/beavers", nil, team{"beavers", "CO"}},
				&store.QueryResult{0, "teams/cardinals", nil, team{"cardinals", "CA"}},
				&store.QueryResult{0, "teams/sharks", nil, team{"sharks", "NY"}},
			},
		},
		{
			"'teams/*' | type team | sort(Location, Name)",
			[]*store.QueryResult{
				&store.QueryResult{0, "teams/cardinals", nil, team{"cardinals", "CA"}},
				&store.QueryResult{0, "teams/bears", nil, team{"bears", "CO"}},
				&store.QueryResult{0, "teams/beavers", nil, team{"beavers", "CO"}},
				&store.QueryResult{0, "teams/sharks", nil, team{"sharks", "NY"}},
			},
		},
		{
			"'teams/*' | type team | sort(Location)",
			[]*store.QueryResult{
				&store.QueryResult{0, "teams/cardinals", nil, team{"cardinals", "CA"}},
				&store.QueryResult{0, "teams/bears", nil, team{"bears", "CO"}},
				&store.QueryResult{0, "teams/beavers", nil, team{"beavers", "CO"}},
				&store.QueryResult{0, "teams/sharks", nil, team{"sharks", "NY"}},
			},
		},
		{
			"'teams/*' | type team | sort(+Location)",
			[]*store.QueryResult{
				&store.QueryResult{0, "teams/cardinals", nil, team{"cardinals", "CA"}},
				&store.QueryResult{0, "teams/bears", nil, team{"bears", "CO"}},
				&store.QueryResult{0, "teams/beavers", nil, team{"beavers", "CO"}},
				&store.QueryResult{0, "teams/sharks", nil, team{"sharks", "NY"}},
			},
		},
		{
			"'teams/*' | type team | sort(-Location)",
			[]*store.QueryResult{
				&store.QueryResult{0, "teams/sharks", nil, team{"sharks", "NY"}},
				&store.QueryResult{0, "teams/bears", nil, team{"bears", "CO"}},
				&store.QueryResult{0, "teams/beavers", nil, team{"beavers", "CO"}},
				&store.QueryResult{0, "teams/cardinals", nil, team{"cardinals", "CA"}},
			},
		},
		{
			"'teams/*' | type team | sort(-Location, Name)",
			[]*store.QueryResult{
				&store.QueryResult{0, "teams/sharks", nil, team{"sharks", "NY"}},
				&store.QueryResult{0, "teams/bears", nil, team{"bears", "CO"}},
				&store.QueryResult{0, "teams/beavers", nil, team{"beavers", "CO"}},
				&store.QueryResult{0, "teams/cardinals", nil, team{"cardinals", "CA"}},
			},
		},
		{
			"'teams/*' | type team | sort(-Location, -Name)",
			[]*store.QueryResult{
				&store.QueryResult{0, "teams/sharks", nil, team{"sharks", "NY"}},
				&store.QueryResult{0, "teams/beavers", nil, team{"beavers", "CO"}},
				&store.QueryResult{0, "teams/bears", nil, team{"bears", "CO"}},
				&store.QueryResult{0, "teams/cardinals", nil, team{"cardinals", "CA"}},
			},
		},
		{
			"'players/*' | type player | sort(Age)",
			[]*store.QueryResult{
				&store.QueryResult{0, "players/alice", nil, player{"alice", 16}},
				&store.QueryResult{0, "players/alfred", nil, player{"alfred", 17}},
				&store.QueryResult{0, "players/bob", nil, player{"bob", 21}},
				&store.QueryResult{0, "players/betty", nil, player{"betty", 23}},
			},
		},
		{
			"'players/*' | type player | sort(-Age)",
			[]*store.QueryResult{
				&store.QueryResult{0, "players/betty", nil, player{"betty", 23}},
				&store.QueryResult{0, "players/bob", nil, player{"bob", 21}},
				&store.QueryResult{0, "players/alfred", nil, player{"alfred", 17}},
				&store.QueryResult{0, "players/alice", nil, player{"alice", 16}},
			},
		},
	}
	for _, test := range tests {
		it := Eval(st.Snapshot(), rootPublicID, storage.ParsePath(""), query.Query{test.query})
		i := 0
		for it.Next() {
			result := it.Get()
			if i >= len(test.expectedResults) {
				t.Errorf("query: %s; not enough expected results (%d); found %v", test.query, len(test.expectedResults), result)
				break
			}
			if got, want := result, test.expectedResults[i]; !reflect.DeepEqual(got, want) {
				t.Errorf("query: %s;\nGOT  %v\nWANT %v", test.query, got, want)
			}
			i++
		}
		if it.Err() != nil {
			t.Errorf("query: %s, Error during eval: %v", test.query, it.Err())
			continue
		}
		if i != len(test.expectedResults) {
			t.Errorf("query: %s, Got %d results, expected %d", test.query, i, len(test.expectedResults))
			continue
		}
		// Ensure that all the goroutines are cleaned up.
		it.(*evalIterator).wait()
	}
}

func TestSelection(t *testing.T) {
	st := populate(t)

	type testCase struct {
		suffix          string
		query           string
		expectedResults []*store.QueryResult
	}

	tests := []testCase{
		{
			"", "'teams/cardinals' | {Name}",
			[]*store.QueryResult{
				&store.QueryResult{0, "teams/cardinals", map[string]vdl.Any{"Name": "cardinals"}, nil},
			},
		},
		{
			"teams", "'cardinals' | {Name}",
			[]*store.QueryResult{
				&store.QueryResult{0, "cardinals", map[string]vdl.Any{"Name": "cardinals"}, nil},
			},
		},
		{
			"teams/cardinals", ". | {Name}",
			[]*store.QueryResult{
				&store.QueryResult{0, "", map[string]vdl.Any{"Name": "cardinals"}, nil},
			},
		},
		{
			"", "'teams/cardinals' | {Name as Name}",
			[]*store.QueryResult{
				&store.QueryResult{0, "teams/cardinals", map[string]vdl.Any{"Name": "cardinals"}, nil},
			},
		},
		{
			"", "'teams/cardinals' | {Name as myname, Location as myloc}",
			[]*store.QueryResult{
				&store.QueryResult{
					0,
					"teams/cardinals",
					map[string]vdl.Any{
						"myname": "cardinals",
						"myloc":  "CA",
					},
					nil},
			},
		},
		{
			"", "'teams/cardinals' | {Name as myname, Location as myloc} | ? myname == 'cardinals'",
			[]*store.QueryResult{
				&store.QueryResult{
					0,
					"teams/cardinals",
					map[string]vdl.Any{
						"myname": "cardinals",
						"myloc":  "CA",
					},
					nil},
			},
		},
	}
	for _, test := range tests {
		it := Eval(st.Snapshot(), rootPublicID, storage.ParsePath(test.suffix), query.Query{test.query})
		i := 0
		for it.Next() {
			result := it.Get()
			if got, want := result, test.expectedResults[i]; !reflect.DeepEqual(got, want) {
				t.Errorf("query: %s;\nGOT  %s\nWANT %s", test.query, got, want)
			}
			i++
		}
		if it.Err() != nil {
			t.Errorf("query: %s, Error during eval: %v", test.query, it.Err())
			continue
		}
		if i != len(test.expectedResults) {
			t.Errorf("query: %s, Got %d results, expected %d", test.query, i, len(test.expectedResults))
			continue
		}
		// Ensure that all the goroutines are cleaned up.
		it.(*evalIterator).wait()
	}
}

func TestError(t *testing.T) {
	st := populate(t)

	type testCase struct {
		query         string
		expectedError string
	}

	tests := []testCase{
		{"teams!foo", "1:6: syntax error at token '!'"},
		{"teams | ?Name > 'foo'", "could not look up name 'Name' relative to 'teams': not found"},
		// This query results in an error because not all of the intermediate
		// results produced by "teams/*" are of type 'team'.
		// TODO(kash): We probably want an error message that says that you must
		// use a type filter.
		{"teams/* | ?Name > 'foo'", "could not look up name 'Name' relative to 'teams': not found"},
		{"'teams/cardinals' | {Name as myname, Location as myloc} | ? Name == 'foo'", "name 'Name' was not selected from 'teams/cardinals', found: [myloc, myname]"},
		{"teams/* | type team | sort(Name) | ?-Name > 'foo'", "cannot negate value of type string for teams/bears"},

		// TODO(kash): Selection with conflicting names.
		// TODO(kash): Trying to sort an aggregate.  "... | avg | sort()"
	}
	for _, test := range tests {
		it := Eval(st.Snapshot(), rootPublicID, storage.PathName{}, query.Query{test.query})
		for it.Next() {
		}
		if it.Err() == nil {
			t.Errorf("query %s, No error, expected %s", test.query, test.expectedError)
			continue
		}
		if it.Err().Error() != test.expectedError {
			t.Errorf("query %s, got error \"%s\", expected \"%s\"", test.query, it.Err(), test.expectedError)
			continue
		}
	}
}

type mockSnapshot struct {
	it state.Iterator
}

func (m *mockSnapshot) NewIterator(pid security.PublicID, path storage.PathName, filter state.IterFilter) state.Iterator {
	return m.it
}

func (m *mockSnapshot) PathMatch(pid security.PublicID, id storage.ID, regex *state.PathRegex) bool {
	return false
}

func (m *mockSnapshot) Find(id storage.ID) *state.Cell {
	return nil
}

func (m *mockSnapshot) Get(pid security.PublicID, path storage.PathName) (*storage.Entry, error) {
	return nil, nil
}

type repeatForeverIterator struct {
	entry    *storage.Entry
	snapshot state.Snapshot
}

func (it *repeatForeverIterator) IsValid() bool {
	return true
}

func (it *repeatForeverIterator) Get() *storage.Entry {
	return it.entry
}

func (it *repeatForeverIterator) Name() string {
	return fmt.Sprintf("teams/%v", it.entry.Stat.MTime)
}

func (it *repeatForeverIterator) Next() {
	it.entry = &storage.Entry{storage.Stat{storage.NewID(), time.Now(), nil}, it.entry.Value}
}

func (it *repeatForeverIterator) Snapshot() state.Snapshot {
	return it.snapshot
}

func TestEvalAbort(t *testing.T) {
	type testCase struct {
		query string
	}

	tests := []testCase{
		testCase{"teams/*"},
		testCase{"teams/* | type team"},
		testCase{"teams/* | ?true"},
	}

	dummyTeam := team{"cardinals", "CA"}
	sn := &mockSnapshot{
		&repeatForeverIterator{
			entry: &storage.Entry{
				storage.Stat{storage.NewID(), time.Now(), nil},
				dummyTeam,
			},
		},
	}
	sn.it.(*repeatForeverIterator).snapshot = sn

	for _, test := range tests {
		// Test calling Abort immediately vs waiting until the channels are full.
		for i := 0; i < 2; i++ {
			it := Eval(sn, rootPublicID, storage.PathName{}, query.Query{test.query})
			if i == 0 {
				// Give the evaluators time to fill up the channels.  Ensure that they
				// don't block forever on a full channel.
				for len(it.(*evalIterator).results) < maxChannelSize {
					time.Sleep(time.Millisecond)
				}
			}
			it.Abort()
			if it.Err() != nil {
				t.Errorf("query:%q Got non-nil error: %v", test.query, it.Err())
			}
			it.(*evalIterator).wait()
		}
	}
}

// TODO(kash): Add a test for access control.
