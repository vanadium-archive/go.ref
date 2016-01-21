// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package raft

import (
	"os"
	"testing"
	"time"

	"v.io/x/lib/vlog"

	"v.io/v23"
	_ "v.io/x/ref/runtime/factories/generic"
)

// waitForElection waits for a new leader to be elected.  We also make sure there
// is only one leader.
func waitForElection(t *testing.T, rs []*raft, timeout time.Duration) *raft {
	start := time.Now()
	for {
		var leader *raft
		leaders := 0
		for _, r := range rs {
			if r.role == roleLeader {
				vlog.Infof("%s is leader", r.me.id)
				leaders++
				leader = r
			}
		}
		if leaders > 1 {
			t.Fatalf("found %d leaders", leaders)
		}
		if leader != nil {
			return leader
		}
		if time.Now().Sub(start) > timeout {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForLeaderAgreement makes sure all working servers agree on the leader.
func waitForLeaderAgreement(rs []*raft, timeout time.Duration) bool {
	start := time.Now()
	for {
		leader := make(map[string]string)
		for _, r := range rs {
			switch r.role {
			case roleLeader:
				leader[r.me.id] = r.me.id
			case roleFollower:
				leader[r.leader] = r.me.id
			}
		}
		if len(leader) == 1 {
			return true
		}
		if time.Now().Sub(start) > timeout {
			vlog.Errorf("oops %v", leader)
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestElection(t *testing.T) {
	vlog.Infof("TestElection")
	ctx, shutdown := v23.Init()
	defer shutdown()

	rs := buildRafts(t, ctx, 5, nil)
	thb := rs[0].heartbeat

	// One of the raft members should time out not hearing a leader and start an election.
	r1 := waitForElection(t, rs, 5*thb)
	if r1 == nil {
		t.Fatalf("too long to find a leader")
	}
	time.Sleep(time.Millisecond)
	if !waitForLeaderAgreement(rs, thb) {
		t.Fatalf("no leader agreement")
	}

	// Stop the leader and wait for the next election.
	r1.Stop()
	r2 := waitForElection(t, rs, 5*thb)
	if r2 == nil {
		t.Fatalf("too long to find a leader")
	}
	if !waitForLeaderAgreement(rs, thb) {
		t.Fatalf("no leader agreement")
	}

	// One more time.
	r2.Stop()
	r3 := waitForElection(t, rs, 5*thb)
	if r3 == nil {
		t.Fatalf("too long to find a leader")
	}
	if !waitForLeaderAgreement(rs, thb) {
		t.Fatalf("no leader agreement")
	}

	// One more time.  Shouldn't succeed since we no longer have a quorum.
	r3.Stop()
	r4 := waitForElection(t, rs, 5*thb)
	if r4 != nil {
		t.Fatalf("shouldn't have a leader with no quorum")
	}

	// Restart r1.  We should be back to a quorum so an election should succeed.
	restart(t, ctx, rs, r1)
	r4 = waitForElection(t, rs, 5*thb)
	if r4 == nil {
		t.Fatalf("too long to find a leader")
	}
	if !waitForLeaderAgreement(rs, thb) {
		t.Fatalf("no leader agreement")
	}

	// Restart r2.  Within thb time the new guy should agree with everyone else on who the leader is.
	restart(t, ctx, rs, r2)
	if !waitForLeaderAgreement(rs, 3*thb) {
		t.Fatalf("no leader agreement")
	}

	// Clean up.
	for i := range rs {
		rs[i].Stop()
		os.RemoveAll(rs[i].logDir)
	}
	vlog.Infof("TestElection passed")
}

func TestPerformanceElection(t *testing.T) {
	vlog.Infof("TestPerformanceElection")
	ctx, shutdown := v23.Init()
	defer shutdown()

	rs := buildRafts(t, ctx, 5, nil)
	thb := rs[0].heartbeat

	// One of the raft members should time out not hearing a leader and start an election.
	r1 := waitForElection(t, rs, 5*thb)
	if r1 == nil {
		t.Fatalf("too long to find a leader")
	}
	time.Sleep(time.Millisecond)
	if !waitForLeaderAgreement(rs, thb) {
		t.Fatalf("no leader agreement")
	}
	vlog.Infof("starting 1000 elections")
	
	// Now force 1000 elections.
	start := time.Now()
	for i := 0; i < 200; i++ {
		x := i % 5
		rs[x].Lock()
		rs[x].startElection()
		rs[x].Unlock()
		if !waitForLeaderAgreement(rs, 5*thb) {
			t.Fatalf("no leader agreement")
		}
	}
	duration := time.Now().Sub(start)
	vlog.Infof("200 elections took %s", duration)
	vlog.Infof("TestPerformanceElection passed")
}
