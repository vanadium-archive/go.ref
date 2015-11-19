// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"reflect"
	"testing"
	"time"

	"v.io/v23/discovery"
	wire "v.io/v23/services/syncbase/nosql"
	_ "v.io/x/ref/runtime/factories/generic"
	"v.io/x/ref/services/syncbase/server/interfaces"
)

func TestNeighborhoodAwarePeerSelector(t *testing.T) {
	// Set a large value to prevent the syncer from running. Since this test
	// adds fake syncgroup members, if the syncer runs, it will attempt to
	// initiate using this fake and partial member data.
	peerSyncInterval = 1 * time.Hour

	svc := createService(t)
	defer destroyService(t, svc)
	s := svc.sync

	s.newPeerSelector(nil, selectNeighborhoodAware)

	// No peers exist.
	if _, err := s.ps.pickPeer(nil); err == nil {
		t.Fatalf("pickPeer didn't fail")
	}

	// Add one joiner.
	nullInfo := wire.SyncgroupMemberInfo{}
	sg := &interfaces.Syncgroup{
		Name:        "sg",
		Id:          interfaces.GroupId(1234),
		AppName:     "mockapp",
		DbName:      "mockdb",
		Creator:     "mockCreator",
		SpecVersion: "etag-0",
		Spec: wire.SyncgroupSpec{
			Prefixes:    []wire.TableRow{{TableName: "foo", Row: ""}, {TableName: "bar", Row: ""}},
			MountTables: []string{"1/2/3/4", "5/6/7/8"},
		},
		Joiners: map[string]wire.SyncgroupMemberInfo{
			"a": nullInfo,
		},
	}

	tx := svc.St().NewTransaction()
	if err := s.addSyncgroup(nil, tx, NoVersion, true, "", nil, s.id, 1, 1, sg); err != nil {
		t.Fatalf("cannot add syncgroup ID %d, err %v", sg.Id, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("cannot commit adding syncgroup ID %d, err %v", sg.Id, err)
	}

	// Add a few peers to simulate neighborhood.
	s.updateDiscoveryPeer("a", &discovery.Service{Addrs: []string{"aa", "aaa"}})
	s.updateDiscoveryPeer("b", &discovery.Service{Addrs: []string{"bb", "bbb"}})

	s.allMembers = nil // force a refresh of the members.

	peerViaMt := connInfo{relName: "a"}                               // connInfo when peer is reachable via mount table.
	peerViaNh := connInfo{relName: "a", addrs: []string{"aa", "aaa"}} // connInfo when peer is reachable via neighborhood.

	// Test peer selection when there are no failures. It should always
	// return a peer whose connInfo indicates reachability via mount table.
	for i := 0; i < 2; i++ {
		if got, err := s.ps.pickPeer(nil); err != nil || !reflect.DeepEqual(got, peerViaMt) {
			t.Fatalf("pickPeer failed, got %v, want %v, err %v", got, peerViaMt, err)
		}
	}

	// Simulate successful mount table reachability, and hence a peer whose
	// connInfo indicates reachability via mount table will continued to be
	// picked.
	for i := 0; i < 2; i++ {
		curTime := time.Now()
		if err := s.ps.updatePeerFromSyncer(nil, peerViaMt, curTime, false); err != nil {
			t.Fatalf("updatePeerFromSyncer failed, err %v", err)
		}

		want := &peerSyncInfo{attemptTs: curTime}

		ps := s.ps.(*neighborhoodAwarePeerSelector)
		got := ps.peerTbl[peerViaMt.relName]

		checkEqualPeerSyncInfo(t, got, want)

		if got, err := s.ps.pickPeer(nil); err != nil || !reflect.DeepEqual(got, peerViaMt) {
			t.Fatalf("pickPeer failed, got %v, want %v, err %v", got, peerViaMt, err)
		}
	}

	// Simulate that peer is not reachable via mount table.
	for i := 1; i <= 10; i++ {
		curTime := time.Now()
		if err := s.ps.updatePeerFromSyncer(nil, peerViaMt, curTime, true); err != nil {
			t.Fatalf("updatePeerFromSyncer failed, err %v", err)
		}

		want := &peerSyncInfo{numFailuresMountTable: uint64(i), attemptTs: curTime}

		ps := s.ps.(*neighborhoodAwarePeerSelector)
		got := ps.peerTbl[peerViaMt.relName]

		checkEqualPeerSyncInfo(t, got, want)

		wantCount := roundsToBackoff(uint64(i))

		if ps.numFailuresMountTable != uint64(i) || ps.curCount != wantCount {
			t.Fatalf("updatePeerFromSyncer failed, got %v, i %v, wantCount %v", ps, i, wantCount)
		}
	}

	// Given that a peer is not reachable via mount table, a peer from the
	// neighborhood should be picked, independent of its success, until
	// backoff count goes to zero.
	status := true
	maxBackoff := roundsToBackoff(maxFailuresForBackoff)
	for i := 0; i < 64; i++ {
		if got, err := s.ps.pickPeer(nil); err != nil || !reflect.DeepEqual(got, peerViaNh) {
			t.Fatalf("pickPeer failed, got %v, want %v, err %v", got, peerViaNh, err)
		}

		curTime := time.Now()
		if err := s.ps.updatePeerFromSyncer(nil, peerViaNh, curTime, status); err != nil {
			t.Fatalf("updatePeerFromSyncer failed, err %v", err)
		}

		var numFailuresNeighborhood uint64
		if status {
			numFailuresNeighborhood = 1
		}

		want := &peerSyncInfo{
			numFailuresMountTable:   10,
			numFailuresNeighborhood: numFailuresNeighborhood,
			attemptTs:               curTime,
		}

		ps := s.ps.(*neighborhoodAwarePeerSelector)
		got := ps.peerTbl[peerViaMt.relName]

		checkEqualPeerSyncInfo(t, got, want)

		if ps.numFailuresMountTable != 10 || ps.curCount != maxBackoff-uint64(i+1) {
			t.Fatalf("updatePeerFromSyncer failed, got %v %v, i %v", ps.numFailuresMountTable, ps.curCount, i)
		}

		status = !status
	}

	// Given that we picked a peer from the neighborhood until the backoff
	// goes to zero, it is time to try the mount table again.
	if got, err := s.ps.pickPeer(nil); err != nil || !reflect.DeepEqual(got, peerViaMt) {
		t.Fatalf("pickPeer failed, got %v, want %v, err %v", got, peerViaMt, err)
	}
}

func checkEqualPeerSyncInfo(t *testing.T, got, want *peerSyncInfo) {
	if got.numFailuresNeighborhood != want.numFailuresNeighborhood ||
		got.numFailuresMountTable != want.numFailuresMountTable ||
		got.attemptTs != want.attemptTs {
		t.Fatalf("checkEqualPeerSyncInfo failed, got %v, want %v", got, want)
	}
}
