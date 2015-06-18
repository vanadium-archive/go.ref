// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Used to ease the setup of sync test scenarios.
// Parses a sync command file and returns a vector of commands to execute.
// dagReplayCommands() executes the parsed commands at the DAG API level.

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"v.io/v23/context"
)

const (
	addLocal = iota
	addRemote
	linkLocal
	linkRemote
)

type syncCommand struct {
	cmd     int
	oid     string
	version string
	parents []string
	logrec  string
	deleted bool
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

			del, err := strconv.ParseBool(args[8])
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid deleted bit: %s", file, lineno, args[8])
			}
			cmd := syncCommand{
				oid:     args[1],
				version: args[2],
				parents: parents,
				logrec:  args[5],
				deleted: del,
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
