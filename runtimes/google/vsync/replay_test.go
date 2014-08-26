package vsync

// Used to ease the setup of Veyron Sync test scenarios.
// Parses a sync command file and returns a vector of commands to execute.
//
// Used by different test replay engines:
// - dagReplayCommands() executes the parsed commands at the DAG API level.
// - logReplayCommands() executes the parsed commands at the Log API level.

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"

	"veyron/services/store/raw"

	"veyron2/storage"
)

const (
	addLocal = iota
	addRemote
	setDevTable
	linkLocal
	linkRemote
)

type syncCommand struct {
	cmd       int
	objID     storage.ID
	version   raw.Version
	parents   []raw.Version
	logrec    string
	devID     DeviceID
	genVec    GenVector
	continued bool
	deleted   bool
}

func strToObjID(objStr string) (storage.ID, error) {
	var objID storage.ID
	id, err := strconv.ParseUint(objStr, 10, 64)
	if err != nil {
		return objID, err
	}
	idbuf := make([]byte, len(objID))
	if binary.PutUvarint(idbuf, id) == 0 {
		return objID, fmt.Errorf("cannot store ID %d into a binary buffer", id)
	}
	for i := 0; i < len(objID); i++ {
		objID[i] = idbuf[i]
	}
	return objID, nil
}

func strToVersion(verStr string) (raw.Version, error) {
	ver, err := strconv.ParseUint(verStr, 10, 64)
	if err != nil {
		return 0, err
	}
	return raw.Version(ver), nil
}

func strToGenID(genIDStr string) (GenID, error) {
	id, err := strconv.ParseUint(genIDStr, 10, 64)
	if err != nil {
		return 0, err
	}
	return GenID(id), nil
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
			expNargs := 8
			if nargs != expNargs {
				return nil, fmt.Errorf("%s:%d: need %d args instead of %d", file, lineno, expNargs, nargs)
			}
			version, err := strToVersion(args[2])
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid version: %s", file, lineno, args[2])
			}
			var parents []raw.Version
			for i := 3; i <= 4; i++ {
				if args[i] != "" {
					pver, err := strToVersion(args[i])
					if err != nil {
						return nil, fmt.Errorf("%s:%d: invalid parent: %s", file, lineno, args[i])
					}
					parents = append(parents, pver)
				}
			}

			continued, err := strconv.ParseBool(args[6])
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid continued bit: %s", file, lineno, args[6])
			}
			del, err := strconv.ParseBool(args[7])
			if err != nil {
				return nil, fmt.Errorf("%s:%d: invalid deleted bit: %s", file, lineno, args[7])
			}
			cmd := syncCommand{version: version, parents: parents, logrec: args[5], continued: continued, deleted: del}
			if args[0] == "addl" {
				cmd.cmd = addLocal
			} else {
				cmd.cmd = addRemote
			}
			if cmd.objID, err = strToObjID(args[1]); err != nil {
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
				genID, err := strToGenID(kv[1])
				if err != nil {
					return nil, fmt.Errorf("%s:%d: invalid gen ID: %s", file, lineno, kv[1])
				}
				genVec[DeviceID(kv[0])] = genID
			}

			cmd := syncCommand{cmd: setDevTable, devID: DeviceID(args[1]), genVec: genVec}
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

			cmd := syncCommand{version: version, parents: []raw.Version{parent}, logrec: args[5]}
			if args[0] == "linkl" {
				cmd.cmd = linkLocal
			} else {
				cmd.cmd = linkRemote
			}
			if cmd.objID, err = strToObjID(args[1]); err != nil {
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
			err = dag.addNode(cmd.objID, cmd.version, false, cmd.deleted, cmd.parents, cmd.logrec, NoTxID)
			if err != nil {
				return fmt.Errorf("cannot add local node %d:%d to DAG: %v", cmd.objID, cmd.version, err)
			}
			if err := dag.moveHead(cmd.objID, cmd.version); err != nil {
				return fmt.Errorf("cannot move head to %d:%d in DAG: %v", cmd.objID, cmd.version, err)
			}
			dag.flush()

		case addRemote:
			err = dag.addNode(cmd.objID, cmd.version, true, cmd.deleted, cmd.parents, cmd.logrec, NoTxID)
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
