// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package raft

// This package implements the Raft protocol, https://ramcloud.stanford.edu/raft.pdf. The
// logged commands are strings.   If someone wishes a more complex command structure, they
// should use an encoding (e.g. json) into the strings.

import (
	"io"
	"math/rand"
	"sort"
	"sync"
	"time"

	"v.io/x/lib/vlog"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/verror"
)

const pkgPath = "v.io/x/ref/lib.raft"

var (
	errBadAppend     = verror.Register(pkgPath+".errBadAppend", verror.NoRetry, "{1:}{2:} inconsistent append{:_}")
	errAddAfterStart = verror.Register(pkgPath+".errAddAfterStart", verror.NoRetry, "{1:}{2:} adding member after start{:_}")
	errNotLeader     = verror.Register(pkgPath+".errNotLeader", verror.NoRetry, "{1:}{2:} not the leader{:_}")
	errWTF           = verror.Register(pkgPath+".errWTF", verror.NoRetry, "{1:}{2:} internal error{:_}")
	errTimedOut      = verror.Register(pkgPath+".errTimedOut", verror.NoRetry, "{1:}{2:} request timed out{:_}")
	errBadTerm       = verror.Register(pkgPath+".errBadTerm", verror.NoRetry, "{1:}{2:} new term {2} < {3} {:_}")
)

const (
	roleCandidate = iota
	roleFollower
	roleLeader
	roleStopped
)

// member keeps track of another member's state.
type member struct {
	id         string
	nextIndex  Index // Next log index to send to this follower.
	matchIndex Index // Last entry logged by this follower.
	stopped    chan struct{}
	update     chan struct{}
	timer      *time.Timer
}

// memberSlice is used for sorting members by highest logged (matched) entry.
type memberSlice []*member
func (m memberSlice) Len() int { return len(m) }
func (m memberSlice) Less(i, j int) bool { return m[i].matchIndex > m[j].matchIndex }
func (m memberSlice) Swap(i, j int) { m[i], m[j] = m[j], m[i] }

// raft is the implementation of the raft library.
type raft struct {
	sync.Mutex
	s  service
	me *member

	// channel for state machine requests
	req chan interface{}

	// go and vanadium state
	ctx    *context.T
	cancel context.CancelFunc
	client RaftClient
	rng    *rand.Rand
	timer  *time.Timer

	// persistent state
	p       persistent
	logDir  string

	// volatile state
	commitIndex     Index              // Highest index commited
	memberMap       map[string]*member // Map of raft members
	memberSet       memberSlice        // Slice of raft members
	leader          string
	role            int
	stop            chan struct{}
	stopped         chan struct{}
	newMatch        chan struct{}
	newCommit       chan Index
	heartbeat       time.Duration
	quorum          int                // Number of members in addition to leader that are a quorum
	ccv             *sync.Cond         // Commit index has changed
	lcv             *sync.Cond         // Leadership has changed
	
	// what we've applied to the client.
	applyLock	sync.Mutex
	lastIndexApplied     Index              // Highest index applied
	lastTermApplied Term

}

type electionRequest interface{}

// newRaft creates a new raft server.
//  logDir        - the name of the directory in which to persist the log.
//  serverName    - a name for the server to announce itself as in a mount table.  All members should use the 
//                 same name and hence be alternatives for callers.
//  hostPort      - the network address of the server
//  hb            - the interval between heartbeats.  0 means use default.
//  smapshotThreshold - the size the log can reach before we create a snapshot.  0 means use default.
//  client        - callbacks to the client.
func newRaft(ctx *context.T, config *RaftConfig, client RaftClient) (*raft, error) {
	nctx, cancel := context.WithCancel(ctx)
	r := &raft{
		ctx:       nctx,
		cancel:    cancel,
		logDir:    config.LogName,
		req:       make(chan interface{}),
		rng:       rand.New(rand.NewSource(time.Now().UnixNano())),
		client:    client,
		memberMap: make(map[string]*member),
		role:      roleStopped,
		heartbeat: config.Heartbeat,
		newMatch:  make(chan struct{}, 1),
		newCommit: make(chan Index, 1),
	}
	r.ccv = sync.NewCond(r)
	r.lcv = sync.NewCond(r)
	if r.heartbeat == 0 {
		r.heartbeat = 3 * time.Second
	}
	r.AddMember(ctx, config.HostPort)
	r.me = r.memberMap[config.HostPort]

	// Init the log state.
	var err error
	if r.p, err = openPersist(ctx, r, config.SnapshotThreshold); err != nil {
		return nil, err
	}

	// Set up the RPC interface to other members.
	eps, err := r.s.newService(nctx, r, config.ServerName, config.HostPort)
	if err != nil {
		return nil, err
	}

	// If we're not in a namespace, create an global name from the first endpoint.
	r.me.id = config.ServerName
	if r.me.id == "" {
		r.me.id = string(getShortName(eps[0]))
	}
	return r, nil
}

// getShortName will return a /host:port name if possible.  Otherwise it will just return the name
// version of the endpoint.
func getShortName(ep naming.Endpoint) string {
	if ep.Addr().Network() != "tcp" {
		return ep.Name()
	}
	return naming.JoinAddressName(ep.Addr().String(), "")
}

// AddMember adds the id as a raft member.  The id must be a vanadium name.
func (r *raft) AddMember(ctx *context.T, id string) error {
	if r.role != roleStopped {
		// already started
		return verror.New(errAddAfterStart, ctx)
	}
	m := &member{id: id, stopped: make(chan struct{}), update: make(chan struct{})}
	r.memberMap[id] = m
	r.memberSet = append(r.memberSet, m)
	// Quorum has to be more than half the servers.
	r.quorum = (len(r.memberSet) + 1) / 2
	return nil
}

// Id returns the vanadium name of this server.
func (r *raft) Id() string {
	return r.me.id
}

// Start gets the protocol going.
func (r *raft) Start() {
	vlog.Infof("@%s starting", r.me.id)
	r.Lock()
	defer r.Unlock()
	if r.role != roleStopped {
		// already started
		return
	}
	r.timer = time.NewTimer(2 * r.heartbeat)
	r.stop = make(chan struct{})
	r.stopped = make(chan struct{})
	go r.serverEvents()
	for _, m := range r.memberSet {
		if m.id != r.me.id {
			go r.perFollower(m)
		}
	}
	return
}

// Stop ceases all function as a raft server.
func (r *raft) Stop() {
	vlog.Infof("@%s stopping", r.me.id)
	r.cancel()
	r.Lock()
	if r.role == roleStopped {
		r.Unlock()
		return
	}
	r.role = roleStopped
	r.Unlock()
	close(r.stop)
	r.s.stopService()
	r.p.Close()

	// Wait for all associated go routines to terminate.
	<-r.stopped
	for _, m := range r.memberSet {
		if m.id != r.me.id {
			<-m.stopped
		}
	}
	vlog.Infof("@%s stopped", r.me.id)
}

// setRoleAndWatchdogTimer called with r.l locked.
func (r *raft) setRoleAndWatchdogTimer(role int) {
	vlog.VI(2).Infof("@%s %s->%s", r.me.id, roleToString(r.role), roleToString(role))
	r.role = role
	switch role {
	case roleFollower:
		// Wake up any appenders waiting for a commitment.
		r.ccv.Broadcast()
		// Set a timer to start an election if we no longer hear from the leader.
		r.resetTimerFuzzy(2 * r.heartbeat)
	case roleLeader:
		// Set known follower status to default values.
		for _, m := range r.memberSet {
			if m.id != r.me.id {
				m.nextIndex = r.p.LastIndex() + 1
				m.matchIndex = 0
			}
		}
		r.leader = r.me.id
		r.kickFollowers()
	case roleCandidate:
		// Set a timer to restart the election if noone becomes leader.
		r.resetTimerFuzzy(r.heartbeat)
	}
}

// startElection starts a new round of voting.  We do this by incrementing the
// Term and, in parallel, calling each other member to vote.  If we receive a
// majority we win and send a heartbeat to each member.
//
// Someone else many get elected in the middle of the vote so we have to
// make sure we're still a candidate at the end of the voting.
//
// Called with r.Lock set
func (r *raft) startElection() {
	// If we can't get a response in 2 seconds, something is really wrong.
	ctx, _ := context.WithTimeout(r.ctx, time.Duration(2)*time.Second)
	if err := r.p.IncCurrentTerm(); err != nil {
		// If this fails, there's no way to recover.
		vlog.Fatalf("incrementing current term: %s", err)
		return
	}
	vlog.VI(2).Infof("@%s startElection new term %d", r.me.id, r.p.CurrentTerm())

	msg := []interface{}{
		r.p.CurrentTerm(),
		r.me.id,
		r.p.LastTerm(),
		r.p.LastIndex(),
	}
	var members []string
	for k,m := range r.memberMap {
		if m.id == r.me.id {
			continue
		}
		members = append(members, k)
	}
	r.setRoleAndWatchdogTimer(roleCandidate)
	r.p.SetVotedFor(r.me.id)
	r.leader = ""
	r.Unlock()

	// We have to do this outside the lock or the system will deadlock when two members start overlapping votes.
	c := make(chan bool)
	for _, id := range members {
		go func(id string) {
			var term Term
			var ok bool
			client := v23.GetClient(ctx)
			if err := client.Call(ctx, id, "RequestVote", msg, []interface{}{&term, &ok}, options.Preresolved{}); err != nil {
				vlog.VI(2).Infof("@%s sending RequestVote to %s: %s", r.me.id, id, err)
			}
			c <- ok
		}(id)
	}

	// Wait till all the voters have voted or timed out.
	oks := 1 // We vote for ourselves.
	for range members {
		if <-c {
			oks++
		}
	}

	r.Lock()
	// We have to check the role since someone else may have become the leader during the round and
	// made us a follower.
	if oks <= len(members)/2 || r.role != roleCandidate {
		vlog.VI(2).Infof("@%s lost election with %d votes", r.me.id, oks)
		return
	}
	vlog.Infof("@%s won election with %d votes", r.me.id, oks)
	r.setRoleAndWatchdogTimer(roleLeader)
	r.leader = r.me.id
	r.client.LeaderChange(r.me.id, r.me.id)
	r.setMatchIndex(r.me, r.p.LastIndex())
	r.lcv.Broadcast()
}

// applyCommits applies any committed entries.
func (r *raft) applyCommits(commitIndex Index) {
	r.applyLock.Lock()
	defer r.applyLock.Unlock()
	for r.lastIndexApplied < commitIndex {
		le := r.p.Lookup(r.lastIndexApplied+1)
		if le == nil {
			// Commit index is ahead of our highest entry.
			return
		}
		r.client.Apply(le)
		r.lastIndexApplied++
		r.lastTermApplied = le.Term
	}
}

func (r *raft) lastApplied() Index {
	r.applyLock.Lock()
	defer r.applyLock.Unlock()
	return r.lastIndexApplied
}

func (r *raft) resetTimerFuzzy(d time.Duration) {
	fuzz := time.Duration(rand.Int63n(int64(r.heartbeat)))
	r.timer.Reset(d + fuzz)
}

func (r *raft) resetTimer(d time.Duration) {
	r.timer.Reset(d)
}

// serverEvents is a go routine that serializes server events.  This loop performs:
// (1) all changes to commitIndex both as a leader and a follower.
// (2) all application of committed log commands.
// (3) all elections.
func (r *raft) serverEvents() {
	r.Lock()
	r.setRoleAndWatchdogTimer(roleFollower)
	r.Unlock()
	for {
		select {
		case <-r.stop:
			// Terminate.
			close(r.stopped)
			return
		case <-r.timer.C:
			// Start an election whenever either:
			// (1) a follower hasn't heard from the leader in a random interval > 2 * heartbeat.
			// (2) a candidate hasn't won an election or been told anyone else has after hearbeat.
			r.Lock()
			switch r.role {
			case roleCandidate:
				r.startElection()
			case roleFollower:
				r.startElection()
			}
			r.Unlock()
		case <-r.newMatch:
			// This happens whenever we have gotten a reply from a follower.  We do it
			// here rather than in perFollower solely as a matter of taste.
			// Update the commitIndex if needed and apply any newly committed entries.
			r.Lock()
			if r.role != roleLeader {
				r.Unlock()
				continue
			}
			sort.Sort(r.memberSet)
			ci := r.memberSet[r.quorum-1].matchIndex
			if ci <= r.commitIndex {
				r.Unlock()
				continue
			}
			r.commitIndex = ci
			r.Unlock()
			r.applyCommits(ci)
			r.ccv.Broadcast()
			r.kickFollowers()
		case i := <-r.newCommit:
			// Update the commitIndex if needed and apply any newly committed entries.
			r.Lock()
			if r.role != roleFollower {
				r.Unlock()
				continue
			}
			if i > r.commitIndex {
				r.commitIndex = i
			}
			ci := r.commitIndex
			r.Unlock()
			r.applyCommits(ci)
			r.ccv.Broadcast()
		}
	}
}

// makeAppendMsg creates an append message at most 10 entries long.
func (r *raft) makeAppendMsg(m *member) ([]interface{}, int) {
	// Figure out if we know the previous entry.
	prevTerm, prevIndex, ok := r.p.LookupPrevious(m.nextIndex)
	if !ok {
		return nil, 0
	}
	// Collect some log entries to send along.  0 is ok.
	var entries []LogEntry
	for i := 0; i < 10; i++ {
		le := r.p.Lookup(m.nextIndex + Index(i))
		if le == nil {
			break
		}
		entries = append(entries, *le)
	}
	return []interface{}{
		r.p.CurrentTerm(),
		r.me.id,
		prevIndex,
		prevTerm,
		r.commitIndex,
		entries,
	}, len(entries)
}

// updateFollower loops trying to update a follower until the follower is updated or we can't proceed.
// It will always send at least one update so will also act as a heartbeat.
func (r *raft) updateFollower(m *member) {
	// Bring this server up to date.
	r.Lock()
	defer r.Unlock()
	for {
		// If we're not the leader we have no followers.
		if r.role != roleLeader {
			return
		}

		// Collect some log entries starting at m.nextIndex.
		msg, n := r.makeAppendMsg(m)
		if msg == nil {
			// Try sending a snapshot.
			r.Unlock()
			vlog.Infof("@%s sending snapshot to %s", r.me.id, m.id)
			snapIndex, err := r.sendLatestSnapshot(m)
			r.Lock()
			if err != nil {
				// Try again later.
				vlog.Errorf("@%s sending snapshot to %s: %s", r.me.id, m.id, err)
				return
			}
			m.nextIndex = snapIndex+1
			vlog.Infof("@%s sent snapshot to %s", r.me.id, m.id)
			// Try sending anything following the snapshot.
			continue
		}

		// Send to the follower. We drop the lock while we do this. That means we may stop being the
		// leader in the middle of the call but that's OK as long as we check when we get it back.
		r.Unlock()
		ctx, _ := context.WithTimeout(r.ctx, time.Duration(2)*time.Second)
		client := v23.GetClient(ctx)
		err := client.Call(ctx, m.id, "AppendToLog", msg, []interface{}{}, options.Preresolved{})
		r.Lock()
		if r.role != roleLeader {
			// Not leader any more, doesn't matter how he replied.
			return
		}
		
		if err != nil {
			if verror.ErrorID(err) != errOutOfSequence.ID {
				// A problem other than missing entries.  Retry later.
				vlog.Errorf("@%s updating %s: %s", r.me.id, m.id, err)
				return
			}
			// At this point we know that the follower is missing entries pervious to what
			// we just sent.  If we can backup, do it.  Otherwise try sending a snapshot.
			if m.nextIndex <= 1 {
				return
			}
			prev := r.p.Lookup(m.nextIndex - 1)
			if prev == nil {
				return
			}
			// We can back up.
			m.nextIndex = m.nextIndex - 1
			continue
		}

		// The follower appended correctly, update indices and tell the server thread that
		// the commit index may need to change.
		m.nextIndex += Index(n)
		logged := m.nextIndex - 1
		if n > 0 {
			r.setMatchIndex(m, logged)
		}

		// The follower is caught up?
		if m.nextIndex > r.p.LastIndex() {
			return
		}
	}
}

func (r *raft) sendLatestSnapshot(m *member) (Index, error) {
	rd, term, index, err := r.p.OpenLatestSnapshot(r.ctx)
	if err != nil {
		return 0, err
	}
	ctx, _ := context.WithTimeout(r.ctx, time.Duration(5*60)*time.Second)
	client := raftProtoClient(m.id)
	call, err := client.InstallSnapshot(ctx, r.p.CurrentTerm(), r.me.id, term, index, options.Preresolved{})
	if err != nil {
		return 0, err
	}
	sstream := call.SendStream()
	b := make([]byte, 10240)
	for {
		n, err := rd.Read(b)
		if n == 0 && err == io.EOF {
			break
		}
		if err = sstream.Send(b); err != nil {
			return 0, err
		}
	}
	if err := call.Finish(); err != nil {
		return 0, err
	}
	return index, nil
}

// perFollower is a go routine that sequences all messages to a single follower.
//
// This is the only go routine that updates the follower's variables so all changes to
// the member struct are serialized by it.
func (r *raft) perFollower(m *member) {
	m.timer = time.NewTimer(r.heartbeat)
	for {
		select {
		case <-m.timer.C:
			r.updateFollower(m)
			m.timer.Reset(r.heartbeat)
		case <-m.update:
			r.updateFollower(m)
			m.timer.Reset(r.heartbeat)
		case <-r.stop:
			close(m.stopped)
			return
		}
	}
}

// kickFollowers causes each perFollower routine to try to update its followers.
func (r *raft) kickFollowers() {
	for _, m := range r.memberMap {
		select {
		case m.update <- struct{}{}:
		default:
		}
	}
}

// setMatchIndex updates the matchIndex for a member.
//
// called with r locked.
func (r *raft) setMatchIndex(m *member, i Index) {
	m.matchIndex = i
	if  i <= r.commitIndex {
		return
	}
	// Check if we need to change the commit index.
	select {
	case r.newMatch <- struct{}{}:
	default:
	}
}

func minIndex(indices ...Index) Index {
	if len(indices) == 0 {
		return 0
	}
	min := indices[0]
	for _, x := range indices[1:] {
		if x < min {
			min = x
		}
	}
	return min
}

func roleToString(r int) string {
	switch r {
	case roleCandidate:
		return "candidate"
	case roleLeader:
		return "leader"
	case roleFollower:
		return "follower"
	}
	return "?"
}

// Append tells the leader to append to the log.  The first error is the result of the client.Apply.  The second
// is any error from raft.
func (r *raft) Append(ctx *context.T, cmd []byte) (applyErr error, err error) {
	r.Lock()
	leader := r.leader
	r.Unlock()

	for {

		switch leader {
		case r.me.id:
			applyErr, err = r.s.Append(ctx, nil, cmd)
			if err == nil {
				// We were the leader and the entry has now been committed.
				return
			}
			// If we're the leader and can't do it, give up.
			if verror.ErrorID(err) != errNotLeader.ID {
				return nil, err
			}
		case "":
			break
		default:
			client := v23.GetClient(ctx)
			if err = client.Call(ctx, leader, "Append", []interface{}{cmd}, []interface{}{&applyErr}, options.Preresolved{}); err == nil {
				return
			}
		}

		// Give up if the caller doesn't want to wait.
		select {
		case <-ctx.Done():
			err = verror.New(errTimedOut, ctx)
			return 
		default:
		}

		// If there is no leader, wait for an election to happen.
		r.Lock()
		if r.leader == "" {
			r.lcv.Wait()
		}
		leader = r.leader
		r.Unlock()
	}
}

func (r *raft) Leader() (bool, string) {
	r.Lock()
	defer r.Unlock()
	if r.role == roleLeader {
		return true, r.leader
	}
	return false, r.leader
}
