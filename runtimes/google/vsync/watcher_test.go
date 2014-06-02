package vsync

// Tests for the Veyron Sync watcher.

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"veyron/services/store/raw"

	"veyron2/ipc"
	"veyron2/rt"
	"veyron2/services/watch"
	"veyron2/storage"
)

var (
	info        testInfo
	recvBlocked chan struct{}
)

// testInfo controls the flow through the fake store and fake reply stream used
// to simulate the Watch API.
type testInfo struct {
	failWatch      bool
	failWatchCount int
	failRecv       bool
	failRecvCount  int
	eofRecv        bool
	blockRecv      bool
	watchResmark   []byte
}

// fakeVStore is used to simulate the Watch() API of the store and stubs the other store APIs.
type fakeVStore struct {
}

func (*fakeVStore) GetMethodTags(_ ipc.Context, _ string, _ ...ipc.CallOpt) ([]interface{}, error) {
	panic("not implemented")
}

func (*fakeVStore) UnresolveStep(_ ipc.Context, _ ...ipc.CallOpt) ([]string, error) {
	panic("not implemented")
}

func (*fakeVStore) Signature(_ ipc.Context, _ ...ipc.CallOpt) (ipc.ServiceSignature, error) {
	panic("not implemented")
}

func (v *fakeVStore) Watch(_ ipc.Context, req watch.Request, _ ...ipc.CallOpt) (watch.WatcherWatchStream, error) {
	// If "failWatch" is set, simulate a failed RPC call.
	if info.failWatch {
		info.failWatchCount++
		return nil, fmt.Errorf("fakeWatch forced error: %d", info.failWatchCount)
	}

	// Save the resmark from the Watch request.
	info.watchResmark = req.ResumeMarker

	// Return a fake stream to access the batch of changes (store mutations).
	return newFakeStream(), nil
}

func (*fakeVStore) PutMutations(_ ipc.Context, _ ...ipc.CallOpt) (raw.StorePutMutationsStream, error) {
	panic("not implemented")
}

// fakeStream is used to simulate the reply stream of the Watch() API.
type fakeStream struct {
	canceled chan struct{}
}

func newFakeStream() *fakeStream {
	s := &fakeStream{}
	s.canceled = make(chan struct{})
	return s
}

func (s *fakeStream) Recv() (watch.ChangeBatch, error) {
	var empty watch.ChangeBatch
	// If "failRecv" is set, simulate a failed call.
	if info.failRecv {
		info.failRecvCount++
		return empty, fmt.Errorf("fake recv error on fake stream: %d", info.failRecvCount)
	}

	// If "eofRecv" is set, simulate a closed stream and make sure the next Recv() call blocks.
	if info.eofRecv {
		info.eofRecv, info.blockRecv = false, true
		return empty, io.EOF
	}

	// If "blockRecv" is set, simulate blocking the call until the stream is canceled.
	if info.blockRecv {
		close(recvBlocked)
		<-s.canceled
		return empty, io.EOF
	}

	// Otherwise return a batch of changes, and make sure the next Recv() call returns EOF on the stream.
	// Adjust the resume marker of the change records to follow the one given to the Watch request.
	info.eofRecv = true
	changes := getChangeBatch()

	var lastCount byte
	if info.watchResmark != nil {
		lastCount = info.watchResmark[0]
	}

	for i := range changes.Changes {
		ch := &changes.Changes[i]
		if !ch.Continued {
			lastCount++
			resmark := []byte{lastCount, 0, 0, 0, 0, 0, 0, 0}
			changes.Changes[i].ResumeMarker = resmark
		}
	}

	return changes, nil
}

func (s *fakeStream) Finish() error {
	return nil
}

func (s *fakeStream) Cancel() {
	close(s.canceled)
}

// getChangeBatch returns a batch of store mutations used to simulate the Watch API.
// The batch contains two transactions to verify both new-object creation and the
// mutation of an existing object.
func getChangeBatch() watch.ChangeBatch {
	var batch watch.ChangeBatch

	batch.Changes = []watch.Change{
		// 1st transaction: create "/" and "/a" and "/a/b" as 3 new objects (prior versions are 0).
		watch.Change{
			Name:  "/uid/4c6db51aa740d8c62b90df874503e285",
			State: 0,
			Value: &raw.Mutation{
				ID: storage.ID{0x4c, 0x6d, 0xb5, 0x1a, 0xa7, 0x40, 0xd8, 0xc6,
					0x2b, 0x90, 0xdf, 0x87, 0x45, 0x3, 0xe2, 0x85},
				PriorVersion: 0x0,
				Version:      0x4d65822107fcfd52,
				Value:        "value-root",
				Tags:         nil,
				Dir: []storage.DEntry{
					storage.DEntry{
						Name: "a",
						ID: storage.ID{0x8, 0x2b, 0xc4, 0x2e, 0x15, 0xaf, 0x4f, 0xcf,
							0x61, 0x1d, 0x7f, 0x19, 0xa8, 0xd7, 0x83, 0x1f},
					},
				},
			},
			ResumeMarker: nil,
			Continued:    true,
		},
		watch.Change{
			Name:  "/uid/082bc42e15af4fcf611d7f19a8d7831f",
			State: 0,
			Value: &raw.Mutation{
				ID: storage.ID{0x8, 0x2b, 0xc4, 0x2e, 0x15, 0xaf, 0x4f, 0xcf,
					0x61, 0x1d, 0x7f, 0x19, 0xa8, 0xd7, 0x83, 0x1f},
				PriorVersion: 0x0,
				Version:      0x57e9d1860d1d68d8,
				Value:        "value-a",
				Tags:         nil,
				Dir: []storage.DEntry{
					storage.DEntry{
						Name: "b",
						ID: storage.ID{0x6e, 0x4a, 0x32, 0x7c, 0x29, 0x7d, 0x76, 0xfb,
							0x51, 0x42, 0xb1, 0xb1, 0xd9, 0x5b, 0x2d, 0x7},
					},
				},
			},
			ResumeMarker: nil,
			Continued:    true,
		},
		watch.Change{
			Name:  "/uid/6e4a327c297d76fb5142b1b1d95b2d07",
			State: 0,
			Value: &raw.Mutation{
				ID: storage.ID{0x6e, 0x4a, 0x32, 0x7c, 0x29, 0x7d, 0x76, 0xfb,
					0x51, 0x42, 0xb1, 0xb1, 0xd9, 0x5b, 0x2d, 0x7},
				PriorVersion: 0x0,
				Version:      0x55104dc76695721d,
				Value:        "value-b",
				Tags:         nil,
				Dir:          nil,
			},
			ResumeMarker: nil,
			Continued:    false,
		},

		// 2nd transaction: create "/a/c" as a new object, which also updates "a" (its "Dir" field).
		watch.Change{
			Name:  "/uid/082bc42e15af4fcf611d7f19a8d7831f",
			State: 0,
			Value: &raw.Mutation{
				ID: storage.ID{0x8, 0x2b, 0xc4, 0x2e, 0x15, 0xaf, 0x4f, 0xcf,
					0x61, 0x1d, 0x7f, 0x19, 0xa8, 0xd7, 0x83, 0x1f},
				PriorVersion: 0x57e9d1860d1d68d8,
				Version:      0x365a858149c6e2d1,
				Value:        "value-a",
				Tags:         nil,
				Dir: []storage.DEntry{
					storage.DEntry{
						Name: "b",
						ID: storage.ID{0x6e, 0x4a, 0x32, 0x7c, 0x29, 0x7d, 0x76, 0xfb,
							0x51, 0x42, 0xb1, 0xb1, 0xd9, 0x5b, 0x2d, 0x7},
					},
					storage.DEntry{
						Name: "c",
						ID: storage.ID{0x70, 0xff, 0x65, 0xec, 0xf, 0x82, 0x5f, 0x44,
							0xb6, 0x9f, 0x89, 0x5e, 0xea, 0x75, 0x9d, 0x71},
					},
				},
			},
			ResumeMarker: nil,
			Continued:    true,
		},
		watch.Change{
			Name:  "/uid/70ff65ec0f825f44b69f895eea759d71",
			State: 0,
			Value: &raw.Mutation{
				ID: storage.ID{0x70, 0xff, 0x65, 0xec, 0xf, 0x82, 0x5f, 0x44,
					0xb6, 0x9f, 0x89, 0x5e, 0xea, 0x75, 0x9d, 0x71},
				PriorVersion: 0x0,
				Version:      0x380704bb7b4d7c03,
				Value:        "value-c",
				Tags:         nil,
				Dir:          nil,
			},
			ResumeMarker: nil,
			Continued:    false,
		},
	}

	return batch
}

// initTestDir creates a per-test directory to store the Sync DB files and returns it.
// It also initializes (resets) the test control metadata.
func initTestDir(t *testing.T) string {
	info = testInfo{}
	recvBlocked = make(chan struct{})
	watchRetryDelay = 10 * time.Millisecond
	streamRetryDelay = 5 * time.Millisecond

	path := fmt.Sprintf("%s/sync_test_%d_%d/", os.TempDir(), os.Getpid(), time.Now().UnixNano())
	if err := os.Mkdir(path, 0775); err != nil {
		t.Fatalf("makeTestDir: cannot create directory %s: %s", path, err)
	}
	return path
}

// fakeSyncd creates a Syncd server structure with enough metadata to be used
// in watcher unit tests.  If "withStore" is true, create a fake store entry.
// Otherwise simulate a no-store Sync server.
func fakeSyncd(t *testing.T, storeDir string, withStore bool) *syncd {
	var s *syncd
	if withStore {
		s = newSyncdCore("", "", "fake-dev", storeDir, "", &fakeVStore{}, 0)
	} else {
		s = newSyncdCore("", "", "fake-dev", storeDir, "", nil, 0)
	}
	if s == nil {
		t.Fatal("cannot create a Sync server")
	}
	return s
}

// TestWatcherNoStore tests the watcher without a connection to a local store.
// It verifies that the watcher exits without side-effects.
func TestWatcherNoStore(t *testing.T) {
	dir := initTestDir(t)
	defer os.RemoveAll(dir)

	s := fakeSyncd(t, dir, false)
	s.Close()
}

// TestWatcherRPCError tests the watcher reacting to an error from the Watch() RPC.
// It verifies that the watcher retries the RPC after a delay.
func TestWatcherRPCError(t *testing.T) {
	rt.Init()
	dir := initTestDir(t)
	defer os.RemoveAll(dir)

	info.failWatch = true
	s := fakeSyncd(t, dir, true)

	n := 4
	time.Sleep(time.Duration(n) * watchRetryDelay)

	s.Close()

	if info.failWatchCount != n && info.failWatchCount != (n+1) {
		t.Fatal("Watch() RPC retry count %d instead of %d or %d", info.failWatchCount, n, n+1)
	}
}

// TestWatcherRecvError tests the watcher reacting to an error from the stream receive.
// It verifies that the watcher retries the Watch() RPC after a delay.
func TestWatcherRecvError(t *testing.T) {
	dir := initTestDir(t)
	defer os.RemoveAll(dir)

	info.failRecv = true
	s := fakeSyncd(t, dir, true)

	n := 2
	time.Sleep(time.Duration(n) * streamRetryDelay)

	s.Close()

	if info.failRecvCount != n && info.failRecvCount != (n+1) {
		t.Fatal("Recv() retry count %d instead of %d or %d", info.failRecvCount, n, n+1)
	}
}

// TestWatcherChanges tests the watcher applying changes received from store.
func TestWatcherChanges(t *testing.T) {
	dir := initTestDir(t)
	defer os.RemoveAll(dir)

	s := fakeSyncd(t, dir, true)

	// Wait for the watcher to block on the Recv(), i.e. it finished processing the updates.
	<-recvBlocked

	// Verify the state of the Sync DAG and Device Table before terminating it.
	oidRoot := storage.ID{0x4c, 0x6d, 0xb5, 0x1a, 0xa7, 0x40, 0xd8, 0xc6, 0x2b, 0x90, 0xdf, 0x87, 0x45, 0x3, 0xe2, 0x85}
	oidA := storage.ID{0x8, 0x2b, 0xc4, 0x2e, 0x15, 0xaf, 0x4f, 0xcf, 0x61, 0x1d, 0x7f, 0x19, 0xa8, 0xd7, 0x83, 0x1f}
	oidB := storage.ID{0x6e, 0x4a, 0x32, 0x7c, 0x29, 0x7d, 0x76, 0xfb, 0x51, 0x42, 0xb1, 0xb1, 0xd9, 0x5b, 0x2d, 0x7}
	oidC := storage.ID{0x70, 0xff, 0x65, 0xec, 0xf, 0x82, 0x5f, 0x44, 0xb6, 0x9f, 0x89, 0x5e, 0xea, 0x75, 0x9d, 0x71}

	oids := []storage.ID{oidRoot, oidA, oidB, oidC}
	heads := []storage.Version{0x4d65822107fcfd52, 0x365a858149c6e2d1, 0x55104dc76695721d, 0x380704bb7b4d7c03}

	for i, oid := range oids {
		expHead := heads[i]
		head, err := s.dag.getHead(oid)
		if err != nil {
			t.Errorf("cannot find head node for object %d: %s", oid, err)
		} else if head != expHead {
			t.Errorf("wrong head for object %d: %d instead of %d", oid, head, expHead)
		}
	}

	expResmark := []byte{2, 0, 0, 0, 0, 0, 0, 0}

	if bytes.Compare(s.devtab.head.Resmark, expResmark) != 0 {
		t.Errorf("error in watch device table resume marker: %v instead of %v", s.devtab.head.Resmark, expResmark)
	}

	if bytes.Compare(info.watchResmark, expResmark) != 0 {
		t.Errorf("error in watch call final resume marker: %v instead of %v", info.watchResmark, expResmark)
	}

	s.Close()
}
