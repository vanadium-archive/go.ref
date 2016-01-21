// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package raft

import (
	"io"
	"time"

	"v.io/v23/context"
)

// RaftClient defines the call backs from the Raft library to the application
type RaftClient interface {
	// Apply performs the command in a log entry.
	Apply(le *LogEntry) error

	// SaveToSnapshot requests the application to store a snapshot.
	// On return, raft assumes that it is safe to continue applying
	// commands (e.g., the old database is now copy on write).  The
	// snapshot is done on a send to the response channel.
	SaveToSnapshot(ctx *context.T, wr io.Writer, response chan<- error) error

	// RestoreFromSnapshot requests the application to rebuild its database from the snapshot file 'fn'.
	RestoreFromSnapshot(ctx *context.T, rd io.Reader) error

	// LeaderChange tells the client that leadership may have changed.
	LeaderChange(me, leader string)
}

type Raft interface {
	// AddMember adds a new member to the server set.  "id" is actually a network address for the member,
	// currently host:port.
	AddMember(ctx *context.T, id string) error

	// Id returns the id of this member.
	Id() string

	// Start starts the local server communicating with other members.
	Start()

	// Stop stops the local server communicating with other members.
	Stop()

	// Append appends a new command to the replicated log.  The command will be executed at each member
	// once a quorum has logged it. The Append will terminate once a quorum has been reached.
	Append(ctx *context.T, cmd []byte) (error, error)

	// Leader returns true if we are leader, false if not, plus the host:port of the leader.
	Leader() (bool, string)
}

type RaftConfig struct {
	LogName           string         // Empty means no log.
	HostPort          string         // For RPCs from other members.
	ServerName        string         // Where to mount if not empty.
	Heartbeat         time.Duration  // Time between heartbeats.
	SnapshotThreshold int64          // Approximate log entries between snapshots.
}

// NewRaft creates a new raft server.
func NewRaft(ctx *context.T, config *RaftConfig, client RaftClient) (Raft, error) {
	return newRaft(ctx, config, client)
}
