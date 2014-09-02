package query

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	_ "veyron/lib/testutil"
	"veyron/services/store/memstore/state"

	"veyron2/query"
	"veyron2/security"
	"veyron2/services/store"
	"veyron2/storage"
	"veyron2/vdl/vdlutil"
	"veyron2/vlog"
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

	put(t, sn, "/players/betty/bio", "")
	put(t, sn, "/players/betty/bio/hometown", "Tampa")

	put(t, sn, "/teams", "")
	put(t, sn, "/teams/cardinals", team{"cardinals", "CA"})
	put(t, sn, "/teams/sharks", team{"sharks", "NY"})
	put(t, sn, "/teams/bears", team{"bears", "CO"})

	put(t, sn, "/teams/cardinals/players", "")
	put(t, sn, "/teams/sharks/players", "")
	put(t, sn, "/teams/bears/players", "")

	put(t, sn, "/teams/cardinals/players/alfred", alfredID)
	put(t, sn, "/teams/sharks/players/alice", aliceID)
	put(t, sn, "/teams/sharks/players/betty", bettyID)
	// Call him something different to make sure we are handling
	// paths correctly in subqueries.  We don't want the subquery
	// "teams/sharks | type team | { players/*}" to work with
	// "/players/bob".
	put(t, sn, "/teams/sharks/players/robert", bobID)

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
		// Object names:
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
			if _, ok := names[result.Name]; ok {
				t.Errorf("query: %s, duplicate results for %s", test.query, result.Name)
			}
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

func TestSample(t *testing.T) {
	st := populate(t)

	type testCase struct {
		query            string
		expectedNumNames int
	}

	tests := []testCase{
		{"teams/* | type team | sample(1)", 1},
		{"teams/* | type team | sample(2)", 2},
		{"teams/* | type team | sample(3)", 3},
		{"teams/* | type team | sample(4)", 3}, // Can't sample more values than exist.
	}

	for _, test := range tests {
		it := Eval(st.Snapshot(), rootPublicID, storage.ParsePath(""), query.Query{test.query})
		names := make(map[string]struct{})
		for it.Next() {
			result := it.Get()
			if _, ok := names[result.Name]; ok {
				t.Errorf("query: %s, duplicate results for %s", test.query, result.Name)
			}
			names[result.Name] = struct{}{}
		}
		if it.Err() != nil {
			t.Errorf("query: %s, Error during eval: %v", test.query, it.Err())
			continue
		}
		if len(names) != test.expectedNumNames {
			t.Errorf("query: %s, Wrong number of names.  got %v, wanted %v", test.query, names, test.expectedNumNames)
			continue
		}
		possibleNames := map[string]struct{}{
			"teams/cardinals": struct{}{},
			"teams/sharks":    struct{}{},
			"teams/bears":     struct{}{},
		}
		for name, _ := range names {
			if _, ok := possibleNames[name]; !ok {
				t.Errorf("Did not find '%s' in %v", name, possibleNames)
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
			"", "'teams/cardinals' | {Name: Name}",
			[]*store.QueryResult{
				&store.QueryResult{0, "teams/cardinals", map[string]vdlutil.Any{"Name": "cardinals"}, nil},
			},
		},
		{
			"teams", "'cardinals' | {Name: Name}",
			[]*store.QueryResult{
				&store.QueryResult{0, "cardinals", map[string]vdlutil.Any{"Name": "cardinals"}, nil},
			},
		},
		{
			"teams/cardinals", ". | {Name: Name}",
			[]*store.QueryResult{
				&store.QueryResult{0, "", map[string]vdlutil.Any{"Name": "cardinals"}, nil},
			},
		},
		{
			"", "'teams/cardinals' | {Name: Name}",
			[]*store.QueryResult{
				&store.QueryResult{0, "teams/cardinals", map[string]vdlutil.Any{"Name": "cardinals"}, nil},
			},
		},
		{
			"", "'teams/cardinals' | {myname: Name, myloc: Location}",
			[]*store.QueryResult{
				&store.QueryResult{
					0,
					"teams/cardinals",
					map[string]vdlutil.Any{
						"myname": "cardinals",
						"myloc":  "CA",
					},
					nil,
				},
			},
		},
		{
			"", "'teams/cardinals' | {myname: Name, myloc: Location} | ? myname == 'cardinals'",
			[]*store.QueryResult{
				&store.QueryResult{
					0,
					"teams/cardinals",
					map[string]vdlutil.Any{
						"myname": "cardinals",
						"myloc":  "CA",
					},
					nil,
				},
			},
		},
		{
			"", "'teams/cardinals' | {myname hidden: Name, myloc: Location} | ? myname == 'cardinals'",
			[]*store.QueryResult{
				&store.QueryResult{
					0,
					"teams/cardinals",
					map[string]vdlutil.Any{
						"myloc": "CA",
					},
					nil,
				},
			},
		},
		{
			"", "'teams/cardinals' | {self: ., myname: Name} | ? myname == 'cardinals'",
			[]*store.QueryResult{
				&store.QueryResult{
					0,
					"teams/cardinals",
					map[string]vdlutil.Any{
						"self":   team{"cardinals", "CA"},
						"myname": "cardinals",
					},
					nil,
				},
			},
		},
		{
			"",
			"'teams/*' | type team | {" +
				"    myname: Name," +
				"    drinkers: players/* | type player | ?Age >=21 | sort()," +
				"    nondrinkers: players/* | type player | ?Age < 21 | sort()" +
				"} | sort(myname)",
			[]*store.QueryResult{
				&store.QueryResult{
					0,
					"teams/bears",
					map[string]vdlutil.Any{
						"myname":      "bears",
						"drinkers":    store.NestedResult(1),
						"nondrinkers": store.NestedResult(2),
					},
					nil,
				},
				&store.QueryResult{
					0,
					"teams/cardinals",
					map[string]vdlutil.Any{
						"myname":      "cardinals",
						"drinkers":    store.NestedResult(3),
						"nondrinkers": store.NestedResult(4),
					},
					nil,
				},
				&store.QueryResult{
					4,
					"teams/cardinals/players/alfred",
					nil,
					player{"alfred", 17},
				},
				&store.QueryResult{
					0,
					"teams/sharks",
					map[string]vdlutil.Any{
						"myname":      "sharks",
						"drinkers":    store.NestedResult(5),
						"nondrinkers": store.NestedResult(6),
					},
					nil,
				},
				&store.QueryResult{
					5,
					"teams/sharks/players/betty",
					nil,
					player{"betty", 23},
				},
				&store.QueryResult{
					5,
					"teams/sharks/players/robert",
					nil,
					player{"bob", 21},
				},
				&store.QueryResult{
					6,
					"teams/sharks/players/alice",
					nil,
					player{"alice", 16},
				},
			},
		},
		// Test for selection of a nested name ('bio/hometown').  Only betty has this
		// nested name, so other players should get a nil value.
		{
			"", "'players/*' | type player | {Name: Name, hometown: 'bio/hometown'} | ? Name == 'alfred' || Name == 'betty' | sort()",
			[]*store.QueryResult{
				&store.QueryResult{
					0,
					"players/alfred",
					map[string]vdlutil.Any{
						"Name":     "alfred",
						"hometown": nil,
					},
					nil,
				},
				&store.QueryResult{
					0,
					"players/betty",
					map[string]vdlutil.Any{
						"Name":     "betty",
						"hometown": "Tampa",
					},
					nil,
				},
			},
		},
	}
	for _, test := range tests {
		vlog.VI(1).Infof("Testing %s\n", test.query)
		it := Eval(st.Snapshot(), rootPublicID, storage.ParsePath(test.suffix), query.Query{test.query})
		i := 0
		for it.Next() {
			result := it.Get()
			if i >= len(test.expectedResults) {
				t.Errorf("query: %s, not enough expected results, need at least %d", i)
				it.Abort()
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
		{"'teams/cardinals' | {myname: Name, myloc: Location} | ? Name == 'foo'", "name 'Name' was not selected from 'teams/cardinals', found: [myloc, myname]"},
		{"teams/* | type team | sort(Name) | ?-Name > 'foo'", "cannot negate value of type string for teams/bears"},
		{"teams/* | type team | sample(2, 3)", "1:21: sample expects exactly one integer argument specifying the number of results to include in the sample"},
		{"teams/* | type team | sample(2.0)", "1:21: sample expects exactly one integer argument specifying the number of results to include in the sample"},
		{"teams/* | type team | sample(-1)", "1:21: sample expects exactly one integer argument specifying the number of results to include in the sample"},

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

func (m *mockSnapshot) NewIterator(pid security.PublicID, path storage.PathName, pathFilter state.PathFilter, filter state.IterFilter) state.Iterator {
	return m.it
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
	return fmt.Sprintf("teams/%v", it.entry.Stat.MTimeNS)
}

func (it *repeatForeverIterator) Next() {
	it.entry = &storage.Entry{
		storage.Stat{storage.ObjectKind, storage.NewID(), time.Now().UnixNano(), nil},
		it.entry.Value,
	}
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
				storage.Stat{storage.ObjectKind, storage.NewID(), time.Now().UnixNano(), nil},
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
				for len(it.(*evalIterator).results[0].results) < maxChannelSize {
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
