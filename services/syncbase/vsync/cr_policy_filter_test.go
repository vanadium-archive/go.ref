// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"testing"

	wire "v.io/v23/services/syncbase"
)

var (
	emptyRule   = &wire.CrRule{Resolver: wire.ResolverTypeLastWins}
	tableRule   = &wire.CrRule{TableName: "t1", Resolver: wire.ResolverTypeLastWins}
	table2Rule  = &wire.CrRule{TableName: "t2", Resolver: wire.ResolverTypeAppResolves}
	fooRule     = &wire.CrRule{TableName: "t1", KeyPrefix: "foo", Resolver: wire.ResolverTypeAppResolves}
	foobarRule  = &wire.CrRule{TableName: "t1", KeyPrefix: "foobar", Resolver: wire.ResolverTypeLastWins}
	foobar2Rule = &wire.CrRule{TableName: "t1", KeyPrefix: "foobar", Resolver: wire.ResolverTypeDefer}
	barRule     = &wire.CrRule{TableName: "t1", KeyPrefix: "bar", Resolver: wire.ResolverTypeAppResolves}
)

var (
	schema = &wire.SchemaMetadata{Policy: wire.CrPolicy{
		Rules: []wire.CrRule{
			*emptyRule, *tableRule, *table2Rule, *fooRule, *foobarRule, *foobar2Rule, *barRule,
		}}}
)

type testRule struct {
	forRule     *wire.CrRule
	againstRule *wire.CrRule
	result      bool
}

func TestIsRuleMoreSpecific(t *testing.T) {
	rules := []testRule{
		createTestRule(tableRule, emptyRule, true),
		createTestRule(fooRule, tableRule, true),
		createTestRule(foobarRule, fooRule, true),
		createTestRule(foobar2Rule, foobarRule, true),

		createTestRule(emptyRule, tableRule, false),
		createTestRule(table2Rule, tableRule, false),
		createTestRule(tableRule, fooRule, false),
		createTestRule(fooRule, foobarRule, false),
		createTestRule(fooRule, barRule, false),
	}
	for _, test := range rules {
		if isRuleMoreSpecific(test.forRule, test.againstRule) != test.result {
			t.Errorf("failed test for rule %v against rule %v", test.forRule, test.againstRule)
		}
	}
}

func createTestRule(forRule, against *wire.CrRule, result bool) testRule {
	return testRule{
		forRule:     forRule,
		againstRule: against,
		result:      result,
	}
}

type testOidRule struct {
	oid    string
	rule   *wire.CrRule
	result bool
}

func TestIsRuleApplicable(t *testing.T) {
	rules := []testOidRule{
		makeTestOidRule("t1", "foopie", emptyRule, true),
		makeTestOidRule("t1", "foopie", tableRule, true),
		makeTestOidRule("t1", "foopie", fooRule, true),
		makeTestOidRule("t1", "abc", tableRule, true),
		makeTestOidRule("t1", "fo", tableRule, true),
		makeTestOidRule("t1", "foobar", foobarRule, true),
		makeTestOidRule("t3", "abc", emptyRule, true),

		makeTestOidRule("t1", "foopie", foobarRule, false),
		makeTestOidRule("t1", "foopie", table2Rule, false),
		makeTestOidRule("t1", "foopie", barRule, false),
	}
	for _, test := range rules {
		if isRuleApplicable(test.oid, test.rule) != test.result {
			t.Errorf("failed test for oid %v for rule %v", test.oid, test.rule)
		}
	}
}

func makeTestOidRule(table, row string, rule *wire.CrRule, result bool) testOidRule {
	return testOidRule{
		oid:    makeRowKeyFromParts(table, row),
		rule:   rule,
		result: result,
	}
}

type testResType struct {
	oid    string
	result wire.ResolverType
}

func TestGetResolutionType(t *testing.T) {
	testCases := []testResType{
		testResType{oid: makeRowKeyFromParts("t1", "abc"), result: wire.ResolverTypeLastWins},
		testResType{oid: makeRowKeyFromParts("t1", "fo"), result: wire.ResolverTypeLastWins},
		testResType{oid: makeRowKeyFromParts("t1", "foo"), result: wire.ResolverTypeAppResolves},
		testResType{oid: makeRowKeyFromParts("t1", "foopie"), result: wire.ResolverTypeAppResolves},
		testResType{oid: makeRowKeyFromParts("t1", "foobar"), result: wire.ResolverTypeDefer},
		testResType{oid: makeRowKeyFromParts("t1", "bar"), result: wire.ResolverTypeAppResolves},
		testResType{oid: makeRowKeyFromParts("t2", "abc"), result: wire.ResolverTypeAppResolves},
		testResType{oid: makePermsKeyFromParts("t2", "abc"), result: wire.ResolverTypeLastWins},
		testResType{oid: makeRowKeyFromParts("t3", "abc"), result: wire.ResolverTypeLastWins},
	}
	for _, test := range testCases {
		if actual := getResolutionType(test.oid, schema); actual != test.result {
			t.Errorf("failed test for oid %v, resolverType expected: %v actual: %v", test.oid, test.result, actual)
		}
	}
}

func TestGroupConflictsByType(t *testing.T) {
	updObjMap := map[string]*objConflictState{}
	updObjMap[makeRowKeyFromParts("t1", "abc")] = &objConflictState{isConflict: true}
	updObjMap[makeRowKeyFromParts("t1", "fo")] = &objConflictState{isConflict: true}
	updObjMap[makeRowKeyFromParts("t1", "foo")] = &objConflictState{isConflict: true}
	updObjMap[makeRowKeyFromParts("t1", "foopie")] = &objConflictState{isConflict: true}
	updObjMap[makeRowKeyFromParts("t1", "foobar")] = &objConflictState{isConflict: true}
	updObjMap[makeRowKeyFromParts("t1", "bar")] = &objConflictState{isConflict: true}
	updObjMap[makeRowKeyFromParts("t2", "abc")] = &objConflictState{isConflict: true}
	updObjMap[makeRowKeyFromParts("t3", "abc")] = &objConflictState{isConflict: true}
	iSt := initiationState{updObjects: updObjMap}

	result := iSt.groupConflictsByType(schema)
	verifyTypeGroupMember(t, result, wire.ResolverTypeLastWins, makeRowKeyFromParts("t1", "abc"))
	verifyTypeGroupMember(t, result, wire.ResolverTypeLastWins, makeRowKeyFromParts("t1", "fo"))
	verifyTypeGroupMember(t, result, wire.ResolverTypeLastWins, makeRowKeyFromParts("t3", "abc"))

	verifyTypeGroupMember(t, result, wire.ResolverTypeAppResolves, makeRowKeyFromParts("t1", "foo"))
	verifyTypeGroupMember(t, result, wire.ResolverTypeAppResolves, makeRowKeyFromParts("t1", "foopie"))
	verifyTypeGroupMember(t, result, wire.ResolverTypeAppResolves, makeRowKeyFromParts("t1", "bar"))
	verifyTypeGroupMember(t, result, wire.ResolverTypeAppResolves, makeRowKeyFromParts("t2", "abc"))

	verifyTypeGroupMember(t, result, wire.ResolverTypeDefer, makeRowKeyFromParts("t1", "foobar"))
}

func verifyTypeGroupMember(t *testing.T, result map[wire.ResolverType]map[string]*objConflictState, rtype wire.ResolverType, oid string) {
	if _, ok := result[rtype][oid]; !ok {
		t.Errorf("object with oid: %v was not grouped under type: %v", oid, rtype)
	}
}
