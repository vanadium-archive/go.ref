// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package raft

import (
	"io"
	"v.io/v23/context"
	"v.io/v23/verror"
)

var (
	errOutOfSequence = verror.Register(pkgPath+".errOutOfSequence", verror.NoRetry, "{1:}{2:} append {3}, {4} out of sequence{:_}")
)

// persistent represents all the persistent state for the raft algorithm.
type persistent interface {
	// AppendLog appends cmds to the log starting at Index prevIndex+1.  It will return
	// an error if there does not exist a previous command at index preIndex with term
	// prevTerm.  It will also return an error if we cannot write to the persistent store.
	AppendToLog(ctx *context.T, prevTerm Term, prevIndex Index, entries []LogEntry) error

	// SetCurrentTerm, SetVotedFor, and SetCurrentTermAndVotedFor changes the non-log persistent state.
	SetCurrentTerm(Term) error
	IncCurrentTerm() error
	SetVotedFor(string) error
	SetCurrentTermAndVotedFor(Term, string) error

	// CurrentTerm returns the current Term.
	CurrentTerm() Term

	// LastIndex returns the highest index in the log.
	LastIndex() Index

	// LastTerm returns the highest term in the log.
	LastTerm() Term

	// GetVotedFor returns the current voted for string.
	VotedFor() string

	// Close all state.
	Close()

	// Lookup returns the log entry at that index or nil if none exists.
	Lookup(Index) *LogEntry

	// LookupPrevious returns the index and term preceding Index.  It returns false if there is none.
	LookupPrevious(Index) (Term, Index, bool)

	// TakeSnapshot calls back the client to generate a snapshot and 
	// then trims the log.
	TakeSnapshot(ctx *context.T) error

	// SnapshotFromLeader receives and stores a snapshot from the leader and then restores the client state from the snapshot.
	SnapshotFromLeader(ctx *context.T, lastTermApplied Term, lastIndexApplied Index, call raftProtoInstallSnapshotServerCall) error

	// OpenLatestSnapshot opens the latest snapshot and returns a reader for it.
	OpenLatestSnapshot(ctx *context.T) (io.Reader, Term, Index, error)
}
