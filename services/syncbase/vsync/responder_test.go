// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/watchable"
)

// TestDiffPrefixGenVectors tests diffing prefix gen vectors.
func TestDiffPrefixGenVectors(t *testing.T) {
	svc := createService(t)
	defer destroyService(t, svc)
	s := svc.sync
	s.id = 10 //responder. Initiator is id 11.

	tests := []struct {
		respPVec, initPVec interfaces.PrefixGenVector
		genDiffIn          genRangeVector
		genDiffWant        genRangeVector
	}{
		{ // responder and initiator are at identical vectors.
			respPVec:  interfaces.PrefixGenVector{10: 1, 11: 10, 12: 20, 13: 2},
			initPVec:  interfaces.PrefixGenVector{10: 1, 11: 10, 12: 20, 13: 2},
			genDiffIn: make(genRangeVector),
		},
		{ // responder and initiator are at identical vectors.
			respPVec:  interfaces.PrefixGenVector{10: 0},
			initPVec:  interfaces.PrefixGenVector{10: 0},
			genDiffIn: make(genRangeVector),
		},
		{ // responder has no updates.
			respPVec:  interfaces.PrefixGenVector{10: 0},
			initPVec:  interfaces.PrefixGenVector{10: 5, 11: 10, 12: 20, 13: 8},
			genDiffIn: make(genRangeVector),
		},
		{ // responder and initiator have no updates.
			respPVec:  interfaces.PrefixGenVector{10: 0},
			initPVec:  interfaces.PrefixGenVector{11: 0},
			genDiffIn: make(genRangeVector),
		},
		{ // responder is staler than initiator.
			respPVec:  interfaces.PrefixGenVector{10: 1, 11: 10, 12: 20, 13: 2},
			initPVec:  interfaces.PrefixGenVector{10: 1, 11: 10, 12: 20, 13: 8, 14: 5},
			genDiffIn: make(genRangeVector),
		},
		{ // responder is more up-to-date than initiator for local updates.
			respPVec:    interfaces.PrefixGenVector{10: 5, 11: 10, 12: 20, 13: 2},
			initPVec:    interfaces.PrefixGenVector{10: 1, 11: 10, 12: 20, 13: 2},
			genDiffIn:   make(genRangeVector),
			genDiffWant: genRangeVector{10: &genRange{min: 2, max: 5}},
		},
		{ // responder is fresher than initiator for local updates and one device.
			respPVec:  interfaces.PrefixGenVector{10: 5, 11: 10, 12: 22, 13: 2},
			initPVec:  interfaces.PrefixGenVector{10: 1, 11: 10, 12: 20, 13: 2, 14: 40},
			genDiffIn: make(genRangeVector),
			genDiffWant: genRangeVector{
				10: &genRange{min: 2, max: 5},
				12: &genRange{min: 21, max: 22},
			},
		},
		{ // responder is fresher than initiator in all but one device.
			respPVec:  interfaces.PrefixGenVector{10: 1, 11: 2, 12: 3, 13: 4},
			initPVec:  interfaces.PrefixGenVector{10: 0, 11: 2, 12: 0},
			genDiffIn: make(genRangeVector),
			genDiffWant: genRangeVector{
				10: &genRange{min: 1, max: 1},
				12: &genRange{min: 1, max: 3},
				13: &genRange{min: 1, max: 4},
			},
		},
		{ // initiator has no updates.
			respPVec:  interfaces.PrefixGenVector{10: 1, 11: 2, 12: 3, 13: 4},
			initPVec:  interfaces.PrefixGenVector{},
			genDiffIn: make(genRangeVector),
			genDiffWant: genRangeVector{
				10: &genRange{min: 1, max: 1},
				11: &genRange{min: 1, max: 2},
				12: &genRange{min: 1, max: 3},
				13: &genRange{min: 1, max: 4},
			},
		},
		{ // initiator has no updates, pre-existing diff.
			respPVec: interfaces.PrefixGenVector{10: 1, 11: 2, 12: 3, 13: 4},
			initPVec: interfaces.PrefixGenVector{13: 1},
			genDiffIn: genRangeVector{
				10: &genRange{min: 5, max: 20},
				13: &genRange{min: 1, max: 3},
			},
			genDiffWant: genRangeVector{
				10: &genRange{min: 1, max: 20},
				11: &genRange{min: 1, max: 2},
				12: &genRange{min: 1, max: 3},
				13: &genRange{min: 1, max: 4},
			},
		},
	}

	for _, test := range tests {
		want := test.genDiffWant
		got := test.genDiffIn
		rSt, err := newResponderState(nil, nil, s, interfaces.DeltaReqData{}, "fakeInitiator")
		if err != nil {
			t.Fatalf("newResponderState failed with err %v", err)
		}
		rSt.diff = got
		rSt.diffPrefixGenVectors(test.respPVec, test.initPVec)
		checkEqualDevRanges(t, got, want)
	}
}

// TestSendDeltas tests the computation of the delta bound (computeDeltaBound)
// and if the log records on the wire are correctly ordered (phases 2 and 3 of
// SendDeltas).
func TestSendDataDeltas(t *testing.T) {
	appName := "mockapp"
	dbName := "mockdb"

	tests := []struct {
		respVec, initVec, outVec interfaces.GenVector
		respGen                  uint64
		genDiff                  genRangeVector
		keyPfxs                  []string
	}{
		{ // Identical prefixes, local and remote updates.
			respVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{12: 8},
				"foobar": interfaces.PrefixGenVector{12: 10},
			},
			initVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{11: 5},
				"foobar": interfaces.PrefixGenVector{11: 5},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{10: 5, 12: 8},
				"foobar": interfaces.PrefixGenVector{10: 5, 12: 10},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
				12: &genRange{min: 1, max: 10},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", ""},
		},
		{ // Identical prefixes, local and remote updates.
			respVec: interfaces.GenVector{
				"bar":    interfaces.PrefixGenVector{12: 20},
				"foo":    interfaces.PrefixGenVector{12: 8},
				"foobar": interfaces.PrefixGenVector{12: 10},
			},
			initVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{11: 5},
				"foobar": interfaces.PrefixGenVector{11: 5, 12: 10},
				"bar":    interfaces.PrefixGenVector{10: 5, 11: 5, 12: 5},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{10: 5, 12: 8},
				"foobar": interfaces.PrefixGenVector{10: 5, 12: 10},
				"bar":    interfaces.PrefixGenVector{10: 5, 12: 20},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
				12: &genRange{min: 1, max: 20},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", "bar", "barbaz", ""},
		},
		{ // Non-identical prefixes, local only updates.
			initVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{11: 5},
				"foobar": interfaces.PrefixGenVector{11: 5},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"foo": interfaces.PrefixGenVector{10: 5},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", "", "fo", "fooxyz"},
		},
		{ // Non-identical prefixes, local and remote updates.
			respVec: interfaces.GenVector{
				"f":      interfaces.PrefixGenVector{12: 5, 13: 5},
				"foo":    interfaces.PrefixGenVector{12: 10, 13: 10},
				"foobar": interfaces.PrefixGenVector{12: 20, 13: 20},
			},
			initVec: interfaces.GenVector{
				"foo": interfaces.PrefixGenVector{11: 5, 12: 1},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{10: 5, 12: 10, 13: 10},
				"foobar": interfaces.PrefixGenVector{10: 5, 12: 20, 13: 20},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
				12: &genRange{min: 2, max: 20},
				13: &genRange{min: 1, max: 20},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", "", "fo", "fooxyz"},
		},
		{ // Non-identical prefixes, local and remote updates.
			respVec: interfaces.GenVector{
				"foobar": interfaces.PrefixGenVector{12: 20, 13: 20},
			},
			initVec: interfaces.GenVector{
				"foo": interfaces.PrefixGenVector{11: 5, 12: 1},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{10: 5},
				"foobar": interfaces.PrefixGenVector{10: 5, 12: 20, 13: 20},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
				12: &genRange{min: 2, max: 20},
				13: &genRange{min: 1, max: 20},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", "", "fo", "fooxyz"},
		},
		{ // Non-identical prefixes, local and remote updates.
			respVec: interfaces.GenVector{
				"f": interfaces.PrefixGenVector{12: 20, 13: 20},
			},
			initVec: interfaces.GenVector{
				"foo": interfaces.PrefixGenVector{11: 5, 12: 1},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"foo": interfaces.PrefixGenVector{10: 5, 12: 20, 13: 20},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
				12: &genRange{min: 2, max: 20},
				13: &genRange{min: 1, max: 20},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", "", "fo", "fooxyz"},
		},
		{ // Non-identical interleaving prefixes.
			respVec: interfaces.GenVector{
				"f":      interfaces.PrefixGenVector{12: 20, 13: 10},
				"foo":    interfaces.PrefixGenVector{12: 30, 13: 20},
				"foobar": interfaces.PrefixGenVector{12: 40, 13: 30},
			},
			initVec: interfaces.GenVector{
				"fo":        interfaces.PrefixGenVector{11: 5, 12: 1},
				"foob":      interfaces.PrefixGenVector{11: 5, 12: 10},
				"foobarxyz": interfaces.PrefixGenVector{11: 5, 12: 20},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"fo":     interfaces.PrefixGenVector{10: 5, 12: 20, 13: 10},
				"foo":    interfaces.PrefixGenVector{10: 5, 12: 30, 13: 20},
				"foobar": interfaces.PrefixGenVector{10: 5, 12: 40, 13: 30},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
				12: &genRange{min: 2, max: 40},
				13: &genRange{min: 1, max: 30},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", "", "fo", "foob", "foobarxyz", "fooxyz"},
		},
		{ // Non-identical interleaving prefixes.
			respVec: interfaces.GenVector{
				"fo":        interfaces.PrefixGenVector{12: 20, 13: 10},
				"foob":      interfaces.PrefixGenVector{12: 30, 13: 20},
				"foobarxyz": interfaces.PrefixGenVector{12: 40, 13: 30},
			},
			initVec: interfaces.GenVector{
				"f":      interfaces.PrefixGenVector{11: 5, 12: 1},
				"foo":    interfaces.PrefixGenVector{11: 5, 12: 10},
				"foobar": interfaces.PrefixGenVector{11: 5, 12: 20},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"f":         interfaces.PrefixGenVector{10: 5},
				"fo":        interfaces.PrefixGenVector{10: 5, 12: 20, 13: 10},
				"foob":      interfaces.PrefixGenVector{10: 5, 12: 30, 13: 20},
				"foobarxyz": interfaces.PrefixGenVector{10: 5, 12: 40, 13: 30},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
				12: &genRange{min: 2, max: 40},
				13: &genRange{min: 1, max: 30},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", "", "fo", "foob", "foobarxyz", "fooxyz"},
		},
		{ // Non-identical sibling prefixes.
			respVec: interfaces.GenVector{
				"foo":       interfaces.PrefixGenVector{12: 20, 13: 10},
				"foobarabc": interfaces.PrefixGenVector{12: 40, 13: 30},
				"foobarxyz": interfaces.PrefixGenVector{12: 30, 13: 20},
			},
			initVec: interfaces.GenVector{
				"foo": interfaces.PrefixGenVector{11: 5, 12: 1},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"foo":       interfaces.PrefixGenVector{10: 5, 12: 20, 13: 10},
				"foobarabc": interfaces.PrefixGenVector{10: 5, 12: 40, 13: 30},
				"foobarxyz": interfaces.PrefixGenVector{10: 5, 12: 30, 13: 20},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
				12: &genRange{min: 2, max: 40},
				13: &genRange{min: 1, max: 30},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", "", "foobarabc", "foobarxyz", "foobar123", "fooxyz"},
		},
		{ // Non-identical prefixes, local and remote updates.
			respVec: interfaces.GenVector{
				"barbaz": interfaces.PrefixGenVector{12: 18},
				"f":      interfaces.PrefixGenVector{12: 30, 13: 5},
				"foobar": interfaces.PrefixGenVector{12: 30, 13: 8},
			},
			initVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{11: 5, 12: 5},
				"foobar": interfaces.PrefixGenVector{11: 5, 12: 5},
				"bar":    interfaces.PrefixGenVector{10: 5, 11: 5, 12: 5},
			},
			respGen: 5,
			outVec: interfaces.GenVector{
				"foo":    interfaces.PrefixGenVector{10: 5, 12: 30, 13: 5},
				"foobar": interfaces.PrefixGenVector{10: 5, 12: 30, 13: 8},
				"bar":    interfaces.PrefixGenVector{10: 5},
				"barbaz": interfaces.PrefixGenVector{10: 5, 12: 18},
			},
			genDiff: genRangeVector{
				10: &genRange{min: 1, max: 5},
				12: &genRange{min: 6, max: 30},
				13: &genRange{min: 1, max: 8},
			},
			keyPfxs: []string{"baz", "wombat", "f", "foo", "foobar", "bar", "barbaz", ""},
		},
	}

	for i, test := range tests {
		svc := createService(t)
		s := svc.sync
		s.id = 10 //responder.

		wantDiff, wantVec := test.genDiff, test.outVec
		s.syncState[appDbName(appName, dbName)] = &dbSyncStateInMem{
			data: &localGenInfoInMem{
				gen:        test.respGen,
				checkptGen: test.respGen,
			},
			genvec: test.respVec,
		}

		////////////////////////////////////////
		// Test sending deltas.

		// Insert some log records to bootstrap testing below.
		tRng := rand.New(rand.NewSource(int64(i)))
		var wantRecs []*localLogRec
		st := svc.St()
		tx := st.NewTransaction()
		objKeyPfxs := test.keyPfxs
		j := 0
		for id, r := range wantDiff {
			pos := uint64(tRng.Intn(50) + 100*j)
			for k := r.min; k <= r.max; k++ {
				opfx := objKeyPfxs[tRng.Intn(len(objKeyPfxs))]
				// Create holes in the log records.
				if opfx == "" {
					continue
				}
				okey := makeRowKey(fmt.Sprintf("%s~%x", opfx, tRng.Int()))
				vers := fmt.Sprintf("%x", tRng.Int())
				rec := &localLogRec{
					Metadata: interfaces.LogRecMetadata{Id: id, Gen: k, ObjId: okey, CurVers: vers, UpdTime: time.Now().UTC()},
					Pos:      pos + k,
				}
				if err := putLogRec(nil, tx, logDataPrefix, rec); err != nil {
					t.Fatalf("putLogRec(%d:%d) failed rec %v err %v", id, k, rec, err)
				}
				value := fmt.Sprintf("value_%s", okey)
				if err := watchable.PutAtVersion(nil, tx, []byte(okey), []byte(value), []byte(vers)); err != nil {
					t.Fatalf("PutAtVersion(%d:%d) failed rec %v value %s: err %v", id, k, rec, value, err)
				}

				initPfxs := extractAndSortPrefixes(test.initVec)
				if !filterLogRec(rec, test.initVec, initPfxs) {
					wantRecs = append(wantRecs, rec)
				}
			}
			j++
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("cannot commit putting log rec, err %v", err)
		}

		req := interfaces.DataDeltaReq{
			AppName: appName,
			DbName:  dbName,
			InitVec: test.initVec,
		}

		rSt, err := newResponderState(nil, nil, s, interfaces.DeltaReqData{req}, "fakeInitiator")
		if err != nil {
			t.Fatalf("newResponderState failed with err %v", err)
		}
		d := &dummyResponder{}
		rSt.call = d
		rSt.st, err = rSt.sync.getDbStore(nil, nil, rSt.appName, rSt.dbName)
		if err != nil {
			t.Fatalf("getDbStore failed to get store handle for app/db %v %v", rSt.appName, rSt.dbName)
		}

		err = rSt.computeDataDeltas(nil)
		if err != nil || !reflect.DeepEqual(rSt.outVec, wantVec) {
			t.Fatalf("computeDataDeltas failed (I: %v), (R: %v, %v), got %v, want %v err %v", test.initVec, test.respGen, test.respVec, rSt.outVec, wantVec, err)
		}
		checkEqualDevRanges(t, rSt.diff, wantDiff)

		if err = rSt.sendDataDeltas(nil); err != nil {
			t.Fatalf("sendDataDeltas failed, err %v", err)
		}

		d.diffLogRecs(t, wantRecs, wantVec)

		destroyService(t, svc)
	}
}

//////////////////////////////
// Helpers

type dummyResponder struct {
	gotRecs []*localLogRec
	outVec  interfaces.GenVector
}

func (d *dummyResponder) SendStream() interface {
	Send(item interfaces.DeltaResp) error
} {
	return d
}

func (d *dummyResponder) Send(item interfaces.DeltaResp) error {
	switch v := item.(type) {
	case interfaces.DeltaRespRespVec:
		d.outVec = v.Value
	case interfaces.DeltaRespRec:
		d.gotRecs = append(d.gotRecs, &localLogRec{Metadata: v.Value.Metadata})
	}
	return nil
}

func (d *dummyResponder) Security() security.Call {
	return nil
}

func (d *dummyResponder) Suffix() string {
	return ""
}

func (d *dummyResponder) LocalEndpoint() naming.Endpoint {
	return nil
}

func (d *dummyResponder) RemoteEndpoint() naming.Endpoint {
	return nil
}

func (d *dummyResponder) GrantedBlessings() security.Blessings {
	return security.Blessings{}
}

func (d *dummyResponder) Server() rpc.Server {
	return nil
}

func (d *dummyResponder) diffLogRecs(t *testing.T, wantRecs []*localLogRec, wantVec interfaces.GenVector) {
	if len(d.gotRecs) != len(wantRecs) {
		t.Fatalf("diffLogRecs failed, gotLen %v, wantLen %v\n", len(d.gotRecs), len(wantRecs))
	}
	for i, rec := range d.gotRecs {
		if !reflect.DeepEqual(rec.Metadata, wantRecs[i].Metadata) {
			t.Fatalf("diffLogRecs failed, i %v, got %v, want %v\n", i, rec.Metadata, wantRecs[i].Metadata)
		}
	}
	if !reflect.DeepEqual(d.outVec, wantVec) {
		t.Fatalf("diffLogRecs failed genvector, got %v, want %v\n", d.outVec, wantVec)
	}
}

func checkEqualDevRanges(t *testing.T, s1, s2 genRangeVector) {
	if len(s1) != len(s2) {
		t.Fatalf("len(s1): %v != len(s2): %v", len(s1), len(s2))
	}
	for d1, r1 := range s1 {
		if r2, ok := s2[d1]; !ok || !reflect.DeepEqual(r1, r2) {
			t.Fatalf("Dev %v: r1 %v != r2 %v", d1, r1, r2)
		}
	}
}
