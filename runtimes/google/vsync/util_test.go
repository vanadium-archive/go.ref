package vsync

// Utilities for testing.
import (
	"container/list"
	"fmt"
	"os"
	"time"

	"veyron/services/store/raw"
)

// getFileName generates a filename for a temporary (per unit test) kvdb file.
func getFileName() string {
	return fmt.Sprintf("%s/sync_test_%d_%d", os.TempDir(), os.Getpid(), time.Now().UnixNano())
}

// createTempDir creates a unique temporary directory to store kvdb files.
func createTempDir() (string, error) {
	dir := fmt.Sprintf("%s/sync_test_%d_%d/", os.TempDir(), os.Getpid(), time.Now().UnixNano())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// getFileSize returns the size of a file.
func getFileSize(fname string) int64 {
	finfo, err := os.Stat(fname)
	if err != nil {
		return -1
	}
	return finfo.Size()
}

// dummyStream struct emulates stream of log records received from RPC.
type dummyStream struct {
	l     *list.List
	value LogRec
}

func newStream() *dummyStream {
	ds := &dummyStream{
		l: list.New(),
	}
	return ds
}

func (ds *dummyStream) Advance() bool {
	if ds.l.Len() > 0 {
		ds.value = ds.l.Remove(ds.l.Front()).(LogRec)
		return true
	}
	return false
}

func (ds *dummyStream) Value() LogRec {
	return ds.value
}

func (*dummyStream) Err() error { return nil }

func (ds *dummyStream) Finish() (GenVector, error) {
	return GenVector{}, nil
}

func (ds *dummyStream) Cancel() {
}

func (ds *dummyStream) add(rec LogRec) {
	ds.l.PushBack(rec)
}

// logReplayCommands replays log records parsed from the input file.
func logReplayCommands(log *iLog, syncfile string) (GenVector, error) {
	cmds, err := parseSyncCommands(syncfile)
	if err != nil {
		return nil, err
	}

	var minGens GenVector
	remote := false
	var stream *dummyStream
	for _, cmd := range cmds {
		switch cmd.cmd {
		case addLocal:
			err = log.processWatchRecord(cmd.objID, cmd.version, cmd.parents, &LogValue{Mutation: raw.Mutation{Version: cmd.version}})
			if err != nil {
				return nil, fmt.Errorf("cannot replay local log records %d:%s err %v",
					cmd.objID, cmd.version, err)
			}

		case addRemote:
			// TODO(hpucha): This code is no longer
			// used. Will be deleted when
			// processWatchRecord is moved to watcher.go
			if !remote {
				stream = newStream()
			}
			remote = true
			id, gnum, lsn, err := splitLogRecKey(cmd.logrec)
			if err != nil {
				return nil, err
			}
			rec := LogRec{
				DevID:   id,
				GNum:    gnum,
				LSN:     lsn,
				ObjID:   cmd.objID,
				CurVers: cmd.version,
				Parents: cmd.parents,
				Value:   LogValue{},
			}
			stream.add(rec)
		}
	}

	return minGens, nil
}

// createReplayStream creates a dummy stream of log records parsed from the input file.
func createReplayStream(syncfile string) (*dummyStream, error) {
	cmds, err := parseSyncCommands(syncfile)
	if err != nil {
		return nil, err
	}

	stream := newStream()
	for _, cmd := range cmds {
		id, gnum, lsn, err := splitLogRecKey(cmd.logrec)
		if err != nil {
			return nil, err
		}
		rec := LogRec{
			DevID:   id,
			GNum:    gnum,
			LSN:     lsn,
			ObjID:   cmd.objID,
			CurVers: cmd.version,
			Parents: cmd.parents,
			Value: LogValue{
				Mutation: raw.Mutation{Version: cmd.version},
			},
		}

		switch cmd.cmd {
		case addRemote:
			rec.RecType = NodeRec
		case linkRemote:
			rec.RecType = LinkRec
		default:
			return nil, err
		}
		stream.add(rec)
	}

	return stream, nil
}

// populates the log and dag state as part of state initialization.
func populateLogAndDAG(s *syncd, rec *LogRec) error {
	logKey, err := s.log.putLogRec(rec)
	if err != nil {
		return err
	}
	if err := s.dag.addNode(rec.ObjID, rec.CurVers, false, rec.Parents, logKey, NoTxID); err != nil {
		return err
	}
	if err := s.dag.moveHead(rec.ObjID, rec.CurVers); err != nil {
		return err
	}
	return nil
}

// vsyncInitState initializes log, dag and devtable state obtained from an input trace-like file.
func vsyncInitState(s *syncd, syncfile string) error {
	cmds, err := parseSyncCommands(syncfile)
	if err != nil {
		return err
	}

	var curGen GenID
	genMap := make(map[string]*genMetadata)

	for _, cmd := range cmds {
		switch cmd.cmd {
		case addLocal, addRemote:
			id, gnum, lsn, err := splitLogRecKey(cmd.logrec)
			if err != nil {
				return err
			}
			rec := &LogRec{
				DevID:   id,
				GNum:    gnum,
				LSN:     lsn,
				ObjID:   cmd.objID,
				CurVers: cmd.version,
				Parents: cmd.parents,
				Value:   LogValue{},
			}
			if err := populateLogAndDAG(s, rec); err != nil {
				return err
			}
			key := generationKey(id, gnum)
			if m, ok := genMap[key]; !ok {
				genMap[key] = &genMetadata{
					Pos:    s.log.head.Curorder,
					Count:  1,
					MaxLSN: rec.LSN,
				}
				s.log.head.Curorder++
			} else {
				m.Count++
				if rec.LSN > m.MaxLSN {
					m.MaxLSN = rec.LSN
				}
			}
			if cmd.cmd == addLocal {
				curGen = gnum
			}

		case setDevTable:
			if err := s.devtab.putGenVec(cmd.devID, cmd.genVec); err != nil {
				return err
			}
		}
	}

	// Initializing genMetadata.
	for key, gen := range genMap {
		dev, gnum, err := splitGenerationKey(key)
		if err != nil {
			return err
		}
		if err := s.log.putGenMetadata(dev, gnum, gen); err != nil {
			return err
		}
	}

	// Initializing generation in log header.
	s.log.head.Curgen = curGen + 1

	return nil
}
