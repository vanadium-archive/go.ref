// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Used to ease the setup of sync test scenarios.
// Parses a sync command file and returns a vector of commands to execute.
// dagReplayCommands() executes the parsed commands at the DAG API level.

import (
	"bufio"
	"container/list"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"v.io/v23/context"
	"v.io/v23/vom"
	"v.io/x/ref/services/syncbase/server/interfaces"
	"v.io/x/ref/services/syncbase/server/util"
	"v.io/x/ref/services/syncbase/server/watchable"
)

const (
	addLocal = iota
	addRemote
	linkLocal
	linkRemote
	genvec
)

var (
	constTime = time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)
)

type syncCommand struct {
	cmd        int
	oid        string
	version    string
	parents    []string
	logrec     string
	deleted    bool
	batchId    uint64
	batchCount uint64
	genVec     interfaces.GenVector
}

// parseSyncCommands parses a sync test file and returns its commands.
func parseSyncCommands(file string) ([]syncCommand, error) {
	cmds := []syncCommand{}

	sf, err := os.Open("testdata/" + file)
	if err != nil {
		return nil, err
	}
	defer sf.Close()

	scanner := bufio.NewScanner(sf)
	lineno := 0
	for scanner.Scan() {
		lineno++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line[0] == '#' {
			continue
		}

		args := strings.Split(line, "|")
		nargs := len(args)

		switch args[0] {
		case "addl", "addr":
			expNargs := 9
			if nargs != expNargs {
				return nil, fmt.Errorf("%s:%d: need %d args instead of %d",
					file, lineno, expNargs, nargs)
			}
			var parents []string
			for i := 3; i <= 4; i++ {
				if args[i] != "" {
					parents = append(parents, args[i])
				}
			}

			batchId, err := strconv.ParseUint(args[6], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid batchId: %s", file, lineno, args[6])
			}
			batchCount, err := strconv.ParseUint(args[7], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid batch count: %s", file, lineno, args[7])
			}
			del, err := strconv.ParseBool(args[8])
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid deleted bit: %s", file, lineno, args[8])
			}
			cmd := syncCommand{
				oid:        args[1],
				version:    args[2],
				parents:    parents,
				logrec:     args[5],
				batchId:    batchId,
				batchCount: batchCount,
				deleted:    del,
			}
			if args[0] == "addl" {
				cmd.cmd = addLocal
			} else {
				cmd.cmd = addRemote
			}
			cmds = append(cmds, cmd)

		case "linkl", "linkr":
			expNargs := 6
			if nargs != expNargs {
				return nil, fmt.Errorf("%s:%d: need %d args instead of %d",
					file, lineno, expNargs, nargs)
			}

			if args[3] == "" {
				return nil, fmt.Errorf("%s:%d: parent version not specified", file, lineno)
			}
			if args[4] != "" {
				return nil, fmt.Errorf("%s:%d: cannot specify a 2nd parent: %s",
					file, lineno, args[4])
			}

			cmd := syncCommand{
				oid:     args[1],
				version: args[2],
				parents: []string{args[3]},
				logrec:  args[5],
			}
			if args[0] == "linkl" {
				cmd.cmd = linkLocal
			} else {
				cmd.cmd = linkRemote
			}
			cmds = append(cmds, cmd)

		case "genvec":
			cmd := syncCommand{
				cmd:    genvec,
				genVec: make(interfaces.GenVector),
			}
			for i := 1; i < len(args); i = i + 2 {
				pfx := args[i]
				genVec := make(interfaces.PrefixGenVector)
				for _, elem := range strings.Split(args[i+1], ",") {
					kv := strings.Split(elem, ":")
					if len(kv) != 2 {
						return nil, fmt.Errorf("%s:%d: invalid gen vector key/val: %s", file, lineno, elem)
					}
					dev, err := strconv.ParseUint(kv[0], 10, 64)
					if err != nil {
						return nil, fmt.Errorf("%s:%d: invalid devid: %s", file, lineno, args[i+1])
					}
					gen, err := strconv.ParseUint(kv[1], 10, 64)
					if err != nil {
						return nil, fmt.Errorf("%s:%d: invalid gen: %s", file, lineno, args[i+1])
					}
					genVec[dev] = gen
				}
				cmd.genVec[pfx] = genVec
			}
			cmds = append(cmds, cmd)

		default:
			return nil, fmt.Errorf("%s:%d: invalid operation: %s", file, lineno, args[0])
		}
	}

	err = scanner.Err()
	return cmds, err
}

// dagReplayCommands parses a sync test file and replays its commands, updating
// the DAG structures associated with the sync service.
func (s *syncService) dagReplayCommands(ctx *context.T, syncfile string) (graftMap, error) {
	cmds, err := parseSyncCommands(syncfile)
	if err != nil {
		return nil, err
	}

	st := s.sv.St()
	graft := newGraft()

	for _, cmd := range cmds {
		tx := st.NewTransaction()

		switch cmd.cmd {
		case addLocal:
			err = s.addNode(ctx, tx, cmd.oid, cmd.version, cmd.logrec,
				cmd.deleted, cmd.parents, NoBatchId, nil)
			if err != nil {
				return nil, fmt.Errorf("cannot add local node %s:%s: %v",
					cmd.oid, cmd.version, err)
			}

			if err = moveHead(ctx, tx, cmd.oid, cmd.version); err != nil {
				return nil, fmt.Errorf("cannot move head to %s:%s: %v",
					cmd.oid, cmd.version, err)
			}

		case addRemote:
			err = s.addNode(ctx, tx, cmd.oid, cmd.version, cmd.logrec,
				cmd.deleted, cmd.parents, NoBatchId, graft)
			if err != nil {
				return nil, fmt.Errorf("cannot add remote node %s:%s: %v",
					cmd.oid, cmd.version, err)
			}

		case linkLocal:
			if err = s.addParent(ctx, tx, cmd.oid, cmd.version, cmd.parents[0], nil); err != nil {
				return nil, fmt.Errorf("cannot add local parent %s to node %s:%s: %v",
					cmd.parents[0], cmd.oid, cmd.version, err)
			}

		case linkRemote:
			if err = s.addParent(ctx, tx, cmd.oid, cmd.version, cmd.parents[0], graft); err != nil {
				return nil, fmt.Errorf("cannot add remote parent %s to node %s:%s: %v",
					cmd.parents[0], cmd.oid, cmd.version, err)
			}
		}

		tx.Commit()
	}

	return graft, nil
}

// dummyStream emulates stream of log records received from RPC.
type dummyStream struct {
	l     *list.List
	entry interfaces.DeltaResp
}

func newStream() *dummyStream {
	ds := &dummyStream{
		l: list.New(),
	}
	return ds
}

func (ds *dummyStream) add(entry interfaces.DeltaResp) {
	ds.l.PushBack(entry)
}

func (ds *dummyStream) Advance() bool {
	if ds.l.Len() > 0 {
		ds.entry = ds.l.Remove(ds.l.Front()).(interfaces.DeltaResp)
		return true
	}
	return false
}

func (ds *dummyStream) Value() interfaces.DeltaResp {
	return ds.entry
}

func (ds *dummyStream) RecvStream() interface {
	Advance() bool
	Value() interfaces.DeltaResp
	Err() error
} {
	return ds
}

func (*dummyStream) Err() error { return nil }

func (ds *dummyStream) Finish() error {
	return nil
}

func (ds *dummyStream) Cancel() {
}

func (ds *dummyStream) SendStream() interface {
	Send(item interfaces.DeltaReq) error
	Close() error
} {
	return ds
}

func (ds *dummyStream) Send(item interfaces.DeltaReq) error {
	return nil
}

func (ds *dummyStream) Close() error {
	return nil
}

// replayLocalCommands replays local log records parsed from the input file.
func replayLocalCommands(t *testing.T, s *mockService, syncfile string) {
	cmds, err := parseSyncCommands(syncfile)
	if err != nil {
		t.Fatalf("parseSyncCommands failed with err %v", err)
	}

	tx := s.St().NewTransaction()
	var pos uint64
	for _, cmd := range cmds {
		switch cmd.cmd {
		case addLocal:
			rec := &localLogRec{
				Metadata: createMetadata(t, interfaces.NodeRec, cmd),
				Pos:      pos,
			}
			err = s.sync.processLocalLogRec(nil, tx, rec)
			if err != nil {
				t.Fatalf("processLocalLogRec failed with err %v", err)
			}

			// Add to Store.
			err = watchable.PutVersion(nil, tx, []byte(rec.Metadata.ObjId), []byte(rec.Metadata.CurVers))
			if err != nil {
				t.Fatalf("PutVersion failed with err %v", err)
			}
			err = watchable.PutAtVersion(nil, tx, []byte(rec.Metadata.ObjId), []byte("abc"), []byte(rec.Metadata.CurVers))
			if err != nil {
				t.Fatalf("PutAtVersion failed with err %v", err)
			}

		default:
			t.Fatalf("replayLocalCommands failed with unknown command %v", cmd)
		}
		pos++
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("cannot commit local log records %s, err %v", syncfile, err)
	}
}

// createReplayStream creates a dummy stream of log records parsed from the input file.
func createReplayStream(t *testing.T, syncfile string) *dummyStream {
	cmds, err := parseSyncCommands(syncfile)
	if err != nil {
		t.Fatalf("parseSyncCommands failed with err %v", err)
	}

	stream := newStream()
	start := interfaces.DeltaRespStart{true}
	stream.add(start)

	for _, cmd := range cmds {
		var ty byte
		switch cmd.cmd {
		case genvec:
			gv := interfaces.DeltaRespRespVec{cmd.genVec}
			stream.add(gv)
			continue
		case addRemote:
			ty = interfaces.NodeRec
		case linkRemote:
			ty = interfaces.LinkRec
		default:
			t.Fatalf("createReplayStream unknown command %v", cmd)
		}

		var val string = "abc"
		valbuf, err := vom.Encode(val)
		if err != nil {
			t.Fatalf("createReplayStream encode failed, err %v", err)
		}

		rec := interfaces.DeltaRespRec{interfaces.LogRec{
			Metadata: createMetadata(t, ty, cmd),
			Value:    valbuf,
		}}

		stream.add(rec)
	}
	fin := interfaces.DeltaRespFinish{true}
	stream.add(fin)
	return stream
}

func createMetadata(t *testing.T, ty byte, cmd syncCommand) interfaces.LogRecMetadata {
	id, gen, err := splitLogRecKey(nil, cmd.logrec)
	if err != nil {
		t.Fatalf("createReplayStream splitLogRecKey failed, key %s, err %v", cmd.logrec, gen)
	}
	m := interfaces.LogRecMetadata{
		Id:         id,
		Gen:        gen,
		RecType:    ty,
		ObjId:      util.JoinKeyParts(util.RowPrefix, cmd.oid),
		CurVers:    cmd.version,
		Parents:    cmd.parents,
		UpdTime:    constTime,
		Delete:     cmd.deleted,
		BatchId:    cmd.batchId,
		BatchCount: cmd.batchCount,
	}
	return m
}
