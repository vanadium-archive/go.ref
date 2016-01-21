// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package raft

import (
	"v.io/x/lib/vlog"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/v23/verror"
)

// service is the implementation of the raftProto.  It is only used between members
// to communicate with each other.
type service struct {
	r          *raft
	server     rpc.Server
	serverName string
}

// newService creates a new service for the raft protocol and returns its network endpoints.
func (s *service) newService(ctx *context.T, r *raft, serverName, hostPort string) ([]naming.Endpoint, error) {
	var err error
	s.r = r
	s.serverName = serverName
	ctx = v23.WithListenSpec(ctx, rpc.ListenSpec{Addrs: rpc.ListenAddrs{{"tcp", hostPort}}})
	_, s.server, err = v23.WithNewDispatchingServer(ctx, serverName, s)
	if err != nil {
		return nil, err
	}
	return s.server.Status().Endpoints, nil
}

func (s *service) stopService() {
	s.server.Stop()
}

// Lookup implements rpc.Dispatcher.Lookup.
func (s *service) Lookup(ctx *context.T, name string) (interface{}, security.Authorizer, error) {
	return raftProtoServer(s), s, nil
}

// Authorize allows anything (for now)
func (s *service) Authorize(*context.T, security.Call) error {
	return nil
}

// Members implements raftProto.Members.
func (s *service) Members(ctx *context.T, call rpc.ServerCall) ([]string, error) {
	r := s.r
	r.Lock()
	defer r.Unlock()

	members := []string{r.me.id}
	for m := range r.memberMap {
		members = append(members, m)
	}
	return members, nil
}

// Members implements raftProto.Leader.
func (s *service) Leader(ctx *context.T, call rpc.ServerCall) (string, error) {
	r := s.r
	r.Lock()
	defer r.Unlock()
	return r.leader, nil
}

// RequestVote implements raftProto.RequestVote.
func (s *service) RequestVote(ctx *context.T, call rpc.ServerCall, term Term, candidate string, lastLogTerm Term, lastLogIndex Index) (Term, bool, error) {
	r := s.r

	// The voting needs to be atomic.
	r.Lock()
	defer r.Unlock()

	// An old election?
	if term < r.p.CurrentTerm() {
		return r.p.CurrentTerm(), false, nil
	}

	// If the term is higher than the current election term, then we are into a new election.
	if term > r.p.CurrentTerm() {
		r.setRoleAndWatchdogTimer(roleFollower)
		r.p.SetVotedFor("")
	}
	vf := r.p.VotedFor()

	// Have we already voted for someone else (for example myself)?
	if vf != "" && vf != candidate {
		return r.p.CurrentTerm(), false, nil
	}

	// If we have a more up to date log, ignore the candidate. (RAFT's safety restriction)
	if r.p.LastTerm() > lastLogTerm {
		return r.p.CurrentTerm(), false, nil
	}
	if r.p.LastTerm() == lastLogTerm && r.p.LastIndex() > lastLogIndex {
		return r.p.CurrentTerm(), false, nil
	}

	// Vote for candidate.
	r.p.SetCurrentTermAndVotedFor(term, candidate)
	return r.p.CurrentTerm(), true, nil
}

// AppendToLog implements RaftProto.AppendToLog.
func (s *service) AppendToLog(ctx *context.T, call rpc.ServerCall, term Term, leader string, prevIndex Index, prevTerm Term, leaderCommit Index, entries []LogEntry) error {
	r := s.r

	// The append needs to be atomic.
	r.Lock()

	// The leader has to be at least as up to date as we are.
	if term < r.p.CurrentTerm() {
		r.Unlock()
		return verror.New(errBadTerm, ctx, term, r.p.CurrentTerm())
	}

	// At this point we  accept the sender as leader and become a follower.
	if r.leader != leader {
		vlog.Infof("@%s new leader %s during AppendToLog", r.me.id, leader)
		r.client.LeaderChange(r.me.id, leader)
	}
	r.leader = leader
	r.lcv.Broadcast()
	r.setRoleAndWatchdogTimer(roleFollower)
	r.p.SetVotedFor("")

	// Append if we can.
	if err := r.p.AppendToLog(ctx, prevTerm, prevIndex, entries); err != nil {
		vlog.Errorf("@%s AppendToLog returns %s", r.me.id, err)
		r.Unlock()
		return err
	}
	r.Unlock()
	r.newCommit <- leaderCommit
	return nil
}

// Append implements RaftProto.Append.
func (s *service) Append(ctx *context.T, call rpc.ServerCall, cmd []byte) (error, error) {
	r := s.r
	r.Lock()
	defer r.Unlock()

	if r.role != roleLeader {
		return nil, verror.New(errNotLeader, ctx)
	}

	// Assign an index and term to the log entry.
	le := LogEntry{Term: r.p.CurrentTerm(), Index: r.p.LastIndex()+1, Cmd: cmd}

	// Append to our own log.
	if err := r.p.AppendToLog(ctx, r.p.LastTerm(), r.p.LastIndex(), []LogEntry{le}); err != nil {
		// This shouldn't happen.
		return nil, err
	}

	// Update the fact that we've logged it.
	r.setMatchIndex(r.me, le.Index)

	// Tell each follower to update.
	r.kickFollowers()

	// Wait for commitment. r.ccv is signalled every time the commit index changes.
	for {
		nle := r.p.Lookup(le.Index)
		if nle == nil || nle.Term != le.Term {
			// There was an election and we lost including our uncommitted entries.
			return nil, verror.New(errNotLeader, ctx)
		}
		if r.commitIndex >= le.Index {
			// We're done, entry is committed.
			return le.Error, nil
		}
		r.ccv.Wait()
	}
}

// InstallSnapshot implements RaftProto.InstallSnapshot.
func (s *service) InstallSnapshot(ctx *context.T, call raftProtoInstallSnapshotServerCall, term Term, leader string, appliedTerm Term, appliedIndex Index) error {
	r := s.r

	// The snapshot needs to be atomic.
	r.Lock()
	defer r.Unlock()

	// The leader has to be at least as up to date as we are.
	if term < r.p.CurrentTerm() {
		return verror.New(errBadTerm, ctx, term, r.p.CurrentTerm())
	}

	// At this point we  accept the sender as leader and become a follower.
	if r.leader != leader {
		vlog.Infof("%s new leader %s", r.me.id, leader)
		r.client.LeaderChange(r.me.id, leader)
	}
	r.leader = leader
	r.lcv.Broadcast()
	r.setRoleAndWatchdogTimer(roleFollower)
	r.p.SetVotedFor("")

	// Store the snapshot and restore client from it.
	return r.p.SnapshotFromLeader(ctx, appliedTerm, appliedIndex, call)
}
