package state

import (
	"veyron/services/store/memstore/refs"

	"veyron2/storage"
	"veyron2/verror"
	"veyron2/vom"
)

type LogVersion uint32

const (
	CurrentLogVersion LogVersion = 1
)

var (
	errUnsupportedLogVersion = verror.Internalf("unsupported log version")
)

// header contains the meta-information for a log file.
type header struct {
	// Version is the version of the log file.
	Version LogVersion

	// RootID is the ID for the root value in the initial snapshot.
	RootID storage.ID

	// StateLen is the number of entries in the initial snapshot.
	StateLen uint32

	// Timestamp is the timestamp of the snapshot, in nanoseconds since the epoch.
	Timestamp uint64
}

// value is a single entry in the initial state snapshot.  It corresponds to the
// <cell> type.
type value struct {
	ID      storage.ID
	Value   interface{}
	Dir     []*storage.DEntry
	Version storage.Version
}

func (st *State) Write(enc *vom.Encoder) error {
	// Write the header.
	sn := st.snapshot
	h := header{
		Version:   CurrentLogVersion,
		StateLen:  uint32(sn.idTable.Len()),
		RootID:    sn.rootID,
		Timestamp: st.timestamp,
	}
	if err := enc.Encode(&h); err != nil {
		return err
	}

	// Write the values.
	var err error
	sn.idTable.Iter(func(it interface{}) bool {
		c := it.(*Cell)
		v := value{ID: c.ID, Value: c.Value, Dir: refs.FlattenDir(c.Dir), Version: c.Version}
		err = enc.Encode(v)
		return err == nil
	})
	return err
}

func (st *State) Read(dec *vom.Decoder) error {
	sn := st.snapshot

	var header header
	if err := dec.Decode(&header); err != nil {
		return err
	}
	if header.Version != CurrentLogVersion {
		return errUnsupportedLogVersion
	}

	// Create the state without refcounts.
	t := emptyIDTable
	for i := uint32(0); i < header.StateLen; i++ {
		var v value
		if err := dec.Decode(&v); err != nil {
			return err
		}
		d := refs.BuildDir(v.Dir)

		// Calculate refs.
		r := refs.NewBuilder()
		r.AddValue(v.Value)
		r.AddDir(d)

		// Add the cell.
		c := &Cell{ID: v.ID, Value: v.Value, Dir: d, Version: v.Version, refs: r.Get(), inRefs: refs.Empty}
		t = t.Put(c)
	}

	sn.idTable = t
	sn.rootID = header.RootID
	st.timestamp = header.Timestamp

	// Update refcounts.
	t.Iter(func(it interface{}) bool {
		c := it.(*Cell)
		sn.addRefs(c.ID, c.refs)
		return true
	})
	return nil
}
