// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"reflect"
	"testing"
	"time"

	"v.io/v23/discovery"
	_ "v.io/x/ref/runtime/factories/generic"
)

func TestPeerDiscovery(t *testing.T) {
	// Set a large value to prevent the syncer and the peer manager from
	// running because this test adds fake neighborhood information.
	peerSyncInterval = 1 * time.Hour
	peerManagementInterval = 1 * time.Hour

	svc := createService(t)
	defer destroyService(t, svc)
	s := svc.sync

	checkPeers := func(peers map[string]uint32, want map[string]*discovery.Service) {
		got := s.filterDiscoveryPeers(peers)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("filterDiscoveryPeers: wrong data: got %v, want %v", got, want)
		}
	}

	peers := map[string]uint32{"a": 0, "b": 1, "c": 2}

	checkPeers(peers, nil)

	// Add peer neighbors.
	svcA := &discovery.Service{Addrs: []string{"aa", "aaa"}}
	svcB := &discovery.Service{Addrs: []string{"bb", "bbb"}}
	s.updateDiscoveryInfo("a", svcA)
	s.updateDiscoveryInfo("b", svcB)

	checkPeers(peers, map[string]*discovery.Service{"a": svcA, "b": svcB})
	checkPeers(map[string]uint32{"x": 0, "y": 1, "z": 2}, map[string]*discovery.Service{})

	// Remove a neighbor.
	s.updateDiscoveryInfo("a", nil)

	checkPeers(peers, map[string]*discovery.Service{"b": svcB})

	// Remove the other neighbor.
	s.updateDiscoveryInfo("b", nil)

	checkPeers(peers, map[string]*discovery.Service{})
}

func TestSyncgroupDiscovery(t *testing.T) {
	// Set a large value to prevent the syncer and the peer manager from
	// running because this test adds fake neighborhood information.
	peerSyncInterval = 1 * time.Hour
	peerManagementInterval = 1 * time.Hour

	svc := createService(t)
	defer destroyService(t, svc)
	s := svc.sync

	checkSyncgroupAdmins := func(sgName string, want map[string]*discovery.Service) {
		got := s.discoverySyncgroupAdmins(sgName)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("discoverySyncgroupAdmins: wrong data: got %v, want %v", got, want)
		}
	}

	checkSyncgroupAdmins("foo", nil)

	// Add syncgroup admin neighbors.
	svcA := &discovery.Service{Addrs: []string{"aa", "aaa"}}
	svcB := &discovery.Service{Addrs: []string{"bb", "bbb"}}
	svcC := &discovery.Service{Addrs: []string{"cc", "ccc"}}

	s.updateDiscoveryInfo(discoverySyncgroupInstanceId("foo", "a"), svcA)
	s.updateDiscoveryInfo(discoverySyncgroupInstanceId("foo", "b"), svcB)
	s.updateDiscoveryInfo(discoverySyncgroupInstanceId("bar", "c"), svcC)

	checkSyncgroupAdmins("foo", map[string]*discovery.Service{"a": svcA, "b": svcB})
	checkSyncgroupAdmins("bar", map[string]*discovery.Service{"c": svcC})
	checkSyncgroupAdmins("haha", nil)

	// Remove an admin from the "foo" syncgroup.
	s.updateDiscoveryInfo(discoverySyncgroupInstanceId("foo", "a"), nil)

	checkSyncgroupAdmins("foo", map[string]*discovery.Service{"b": svcB})
	checkSyncgroupAdmins("bar", map[string]*discovery.Service{"c": svcC})

	// Remove the other "foo" admin.
	s.updateDiscoveryInfo(discoverySyncgroupInstanceId("foo", "b"), nil)

	checkSyncgroupAdmins("foo", nil)
	checkSyncgroupAdmins("bar", map[string]*discovery.Service{"c": svcC})

	// Remove the other "bar" admin.
	s.updateDiscoveryInfo(discoverySyncgroupInstanceId("bar", "c"), nil)

	checkSyncgroupAdmins("bar", nil)
}
