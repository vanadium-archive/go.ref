// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

// Used to ease the setup of Veyron Sync test scenarios.
// Parses a sync command file and returns a vector of commands to execute.
//
// Used by different test replay engines:
// - dagReplayCommands() executes the parsed commands at the DAG API level.
// - logReplayCommands() executes the parsed commands at the Log API level.

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	addLocal = iota
	addRemote
	setDevTable
	linkLocal
	linkRemote
)

type syncCommand struct {
	cmd     int
	objID   ObjId
	version Version
	parents []Version
	logrec  string
	devID   DeviceId
	genVec  GenVector
	txID    TxId
	txCount uint32
	deleted bool
}

func strToVersion(verStr string) (Version, error) {
	ver, err := strconv.ParseUint(verStr, 10, 64)
	if err != nil {
		return 0, err
	}
	return Version(ver), nil
}

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
				return nil, fmt.Errorf("%s:%d: need %d args instead of %d", file, lineno, expNargs, nargs)
			}
			version, err := strToVersion(args[2])
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid version: %s", file, lineno, args[2])
			}
			var parents []Version
			for i := 3; i <= 4; i++ {
				if args[i] != "" {
					pver, err := strToVersion(args[i])
					if err != nil {
						return nil, fmt.Errorf("%s:%d: invalid parent: %s", file, lineno, args[i])
					}
					parents = append(parents, pver)
				}
			}

			txID, err := strToTxId(args[6])
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid TxId: %s", file, lineno, args[6])
			}
			txCount, err := strconv.ParseUint(args[7], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid tx count: %s", file, lineno, args[7])
			}
			del, err := strconv.ParseBool(args[8])
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid deleted bit: %s", file, lineno, args[8])
			}
			cmd := syncCommand{
				version: version,
				parents: parents,
				logrec:  args[5],
				txID:    txID,
				txCount: uint32(txCount),
				deleted: del,
			}
			if args[0] == "addl" {
				cmd.cmd = addLocal
			} else {
				cmd.cmd = addRemote
			}
			if cmd.objID, err = strToObjId(args[1]); err != nil {
				return nil, fmt.Errorf("%s:%d: invalid object ID: %s", file, lineno, args[1])
			}
			cmds = append(cmds, cmd)

		case "setdev":
			expNargs := 3
			if nargs != expNargs {
				return nil, fmt.Errorf("%s:%d: need %d args instead of %d", file, lineno, expNargs, nargs)
			}

			genVec := make(GenVector)
			for _, elem := range strings.Split(args[2], ",") {
				kv := strings.Split(elem, ":")
				if len(kv) != 2 {
					return nil, fmt.Errorf("%s:%d: invalid gen vector key/val: %s", file, lineno, elem)
				}
				genID, err := strToGenId(kv[1])
				if err != nil {
					return nil, fmt.Errorf("%s:%d: invalid gen ID: %s", file, lineno, kv[1])
				}
				genVec[DeviceId(kv[0])] = genID
			}

			cmd := syncCommand{cmd: setDevTable, devID: DeviceId(args[1]), genVec: genVec}
			cmds = append(cmds, cmd)

		case "linkl", "linkr":
			expNargs := 6
			if nargs != expNargs {
				return nil, fmt.Errorf("%s:%d: need %d args instead of %d", file, lineno, expNargs, nargs)
			}

			version, err := strToVersion(args[2])
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid version: %s", file, lineno, args[2])
			}
			if args[3] == "" {
				return nil, fmt.Errorf("%s:%d: parent (to-node) version not specified", file, lineno)
			}
			if args[4] != "" {
				return nil, fmt.Errorf("%s:%d: cannot specify a 2nd parent (to-node): %s", file, lineno, args[4])
			}
			parent, err := strToVersion(args[3])
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid parent (to-node) version: %s", file, lineno, args[3])
			}

			cmd := syncCommand{version: version, parents: []Version{parent}, logrec: args[5]}
			if args[0] == "linkl" {
				cmd.cmd = linkLocal
			} else {
				cmd.cmd = linkRemote
			}
			if cmd.objID, err = strToObjId(args[1]); err != nil {
				return nil, fmt.Errorf("%s:%d: invalid object ID: %s", file, lineno, args[1])
			}
			cmds = append(cmds, cmd)

		default:
			return nil, fmt.Errorf("%s:%d: invalid operation: %s", file, lineno, args[0])
		}
	}

	err = scanner.Err()
	return cmds, err
}

func dagReplayCommands(dag *dag, syncfile string) error {
	cmds, err := parseSyncCommands(syncfile)
	if err != nil {
		return err
	}

	for _, cmd := range cmds {
		switch cmd.cmd {
		case addLocal:
			err = dag.addNode(cmd.objID, cmd.version, false, cmd.deleted, cmd.parents, cmd.logrec, NoTxId)
			if err != nil {
				return fmt.Errorf("cannot add local node %d:%d to DAG: %v", cmd.objID, cmd.version, err)
			}
			if err := dag.moveHead(cmd.objID, cmd.version); err != nil {
				return fmt.Errorf("cannot move head to %d:%d in DAG: %v", cmd.objID, cmd.version, err)
			}
			dag.flush()

		case addRemote:
			err = dag.addNode(cmd.objID, cmd.version, true, cmd.deleted, cmd.parents, cmd.logrec, NoTxId)
			if err != nil {
				return fmt.Errorf("cannot add remote node %d:%d to DAG: %v", cmd.objID, cmd.version, err)
			}
			dag.flush()

		case linkLocal:
			if err = dag.addParent(cmd.objID, cmd.version, cmd.parents[0], false); err != nil {
				return fmt.Errorf("cannot add local parent %d to DAG node %d:%d: %v",
					cmd.parents[0], cmd.objID, cmd.version, err)
			}
			dag.flush()

		case linkRemote:
			if err = dag.addParent(cmd.objID, cmd.version, cmd.parents[0], true); err != nil {
				return fmt.Errorf("cannot add remote parent %d to DAG node %d:%d: %v",
					cmd.parents[0], cmd.objID, cmd.version, err)
			}
			dag.flush()
		}
	}
	return nil
}
