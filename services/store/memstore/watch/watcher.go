package watch

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"veyron/services/store/memstore"
	"veyron/services/store/service"

	"veyron2/ipc"
	"veyron2/security"
	"veyron2/services/watch"
)

var (
	ErrUnknownResumeMarker    = errors.New("Unknown ResumeMarker")
	nowResumeMarker           = []byte("now") // UTF-8 conversion.
	initialStateSkippedChange = watch.Change{
		Name:  "",
		State: watch.InitialStateSkipped,
	}
)

type watcher struct {
	admin   security.PublicID
	dbName  string
	closed  chan struct{}
	pending sync.WaitGroup
}

func New(admin security.PublicID, dbName string) (service.Watcher, error) {
	return &watcher{
		admin:  admin,
		dbName: dbName,
		closed: make(chan struct{}),
	}, nil
}

// Watch handles the specified request, processing records in the store log and
// sending changes to the specified watch stream. If the call is cancelled or
// otherwise closed early, Watch will terminate and return an error.
// Watch implements the service.Watcher interface.
func (w *watcher) Watch(ctx ipc.ServerContext, req watch.Request, stream watch.WatcherServiceWatchStream) error {
	processor, err := w.findProcessor(ctx.RemoteID(), req)
	if err != nil {
		return err
	}
	log, err := memstore.OpenLog(w.dbName, true)
	if err != nil {
		return err
	}
	// Close the log when:
	// 1) Watch returns early (with an error). This is signalled on the done channel.
	// 2) The call closes early. This is signalled on the context's closed channel.
	// Closing the log terminates any ongoing read, and Watch returns an error.
	// TODO(tilaks): cancellable processState(), processTransaction().
	done := make(chan struct{})
	defer close(done)
	w.pending.Add(1)
	go func() {
		select {
		case <-done:
		case <-ctx.Closed():
		case <-w.closed:
		}
		log.Close()
		w.pending.Done()
	}()

	resumeMarker := req.ResumeMarker
	if isNowResumeMarker(resumeMarker) {
		sendChanges(stream, []watch.Change{initialStateSkippedChange})
	}
	// Retrieve the initial timestamp. Changes that occured at or before the
	// initial timestamp will not be sent.
	initialTimestamp, err := resumeMarkerToTimestamp(resumeMarker)
	if err != nil {
		return err
	}

	// Process initial state.
	store, err := log.ReadState(w.admin)
	if err != nil {
		return err
	}
	st := store.State
	changes, err := processor.processState(st)
	if err != nil {
		return err
	}
	err = processChanges(stream, changes, initialTimestamp, st.Timestamp())
	if err != nil {
		return err
	}

	for {
		// Process transactions.
		mu, err := log.ReadTransaction()
		if err != nil {
			return err
		}
		changes, err = processor.processTransaction(mu)
		if err != nil {
			return err
		}
		err = processChanges(stream, changes, initialTimestamp, mu.Timestamp)
		if err != nil {
			return err
		}
	}
}

// Close implements the service.Watcher interface.
func (w *watcher) Close() error {
	close(w.closed)
	w.pending.Wait()
	return nil
}

func (w *watcher) findProcessor(client security.PublicID, req watch.Request) (reqProcessor, error) {
	// TODO(tilaks): verify Sync requests.
	// TODO(tilaks): handle application requests.
	return newSyncProcessor(client)
}

func processChanges(stream watch.WatcherServiceWatchStream, changes []watch.Change, initialTimestamp, timestamp uint64) error {
	if timestamp <= initialTimestamp {
		return nil
	}
	addResumeMarkers(changes, timestampToResumeMarker(timestamp))
	return sendChanges(stream, changes)
}

func sendChanges(stream watch.WatcherServiceWatchStream, changes []watch.Change) error {
	if len(changes) == 0 {
		return nil
	}
	// TODO(tilaks): batch more aggressively.
	return stream.Send(watch.ChangeBatch{Changes: changes})
}

func addResumeMarkers(changes []watch.Change, resumeMarker []byte) {
	for i, _ := range changes {
		changes[i].ResumeMarker = resumeMarker
	}
}

func isNowResumeMarker(resumeMarker []byte) bool {
	return bytes.Equal(resumeMarker, nowResumeMarker)
}

func resumeMarkerToTimestamp(resumeMarker []byte) (uint64, error) {
	if len(resumeMarker) == 0 {
		return 0, nil
	}
	if isNowResumeMarker(resumeMarker) {
		// TODO(tilaks): Get the current resume marker from the log.
		return uint64(time.Now().UnixNano()), nil
	}
	if len(resumeMarker) != 8 {
		return 0, ErrUnknownResumeMarker
	}
	return binary.BigEndian.Uint64(resumeMarker), nil
}

func timestampToResumeMarker(timestamp uint64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, timestamp)
	return buf
}
