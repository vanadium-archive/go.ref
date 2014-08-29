package watch

import (
	"bytes"
	"encoding/binary"
	"io"
	"time"

	"veyron/runtimes/google/lib/sync"
	"veyron/services/store/memstore"
	"veyron/services/store/raw"

	"veyron2/ipc"
	"veyron2/security"
	"veyron2/services/watch"
	"veyron2/services/watch/types"
	"veyron2/storage"
	"veyron2/verror"
)

var (
	errWatchClosed            = io.EOF
	errUnknownResumeMarker    = verror.BadArgf("Unknown ResumeMarker")
	nowResumeMarker           = []byte("now") // UTF-8 conversion.
	initialStateSkippedChange = types.Change{
		Name:  "",
		State: types.InitialStateSkipped,
	}
)

type Watcher struct {
	// admin is the public id of the store administrator.
	admin security.PublicID
	// dbName is the name of the store's database directory.
	dbName string
	// closed is a channel that is closed when the watcher is closed.
	// Watch invocations finish as soon as possible once the channel is closed.
	closed chan struct{}
	// pending records the number of Watch invocations on this watcher that
	// have not yet finished.
	pending sync.WaitGroup
}

// New returns a new watcher. The returned watcher supports repeated and
// concurrent invocations of Watch until it is closed.
// admin is the public id of the store administrator. dbName is the name of the
// of the store's database directory.
func New(admin security.PublicID, dbName string) (*Watcher, error) {
	return &Watcher{
		admin:  admin,
		dbName: dbName,
		closed: make(chan struct{}),
	}, nil
}

// WatchRaw returns a stream of all changes.
func (w *Watcher) WatchRaw(ctx ipc.ServerContext, req raw.Request,
	stream raw.StoreServiceWatchStream) error {

	processor, err := newRawProcessor(ctx.RemoteID())
	if err != nil {
		return err
	}
	return w.Watch(ctx, processor, req.ResumeMarker, stream.SendStream())
}

// WatchGlob returns a stream of changes that match a pattern.
func (w *Watcher) WatchGlob(ctx ipc.ServerContext, path storage.PathName,
	req types.GlobRequest, stream watch.GlobWatcherServiceWatchGlobStream) error {

	processor, err := newGlobProcessor(ctx.RemoteID(), path, req.Pattern)
	if err != nil {
		return err
	}
	return w.Watch(ctx, processor, req.ResumeMarker, stream.SendStream())
}

// WatchQuery returns a stream of changes that satisfy a query.
func (w *Watcher) WatchQuery(ctx ipc.ServerContext, path storage.PathName,
	req types.QueryRequest, stream watch.QueryWatcherServiceWatchQueryStream) error {

	return verror.Internalf("WatchQuery not yet implemented")
}

// WatchStream is the interface for streaming responses of Watch methods.
type WatchStream interface {
	// Send places the item onto the output stream, blocking if there is no
	// buffer space available.
	Send(item types.Change) error
}

// Watch handles the specified request, processing records in the store log and
// sending changes to the specified watch stream. If the call is cancelled or
// otherwise closed early, Watch will terminate and return an error.
// Watch implements the service.Watcher interface.
func (w *Watcher) Watch(ctx ipc.ServerContext, processor reqProcessor,
	resumeMarker types.ResumeMarker, stream WatchStream) error {

	// Closing cancel terminates processRequest.
	cancel := make(chan struct{})
	defer close(cancel)

	done := make(chan error, 1)

	if !w.pending.TryAdd() {
		return errWatchClosed
	}
	// This goroutine does not leak because processRequest is always terminated.
	go func() {
		defer w.pending.Done()
		done <- w.processRequest(cancel, processor, resumeMarker, stream)
		close(done)
	}()

	select {
	case err := <-done:
		return err
	// Close cancel and terminate processRequest if:
	// 1) The watcher has been closed.
	// 2) The call closes. This is signalled on the context's closed channel.
	case <-w.closed:
	case <-ctx.Done():
	}
	return errWatchClosed
}

func (w *Watcher) processRequest(cancel <-chan struct{}, processor reqProcessor,
	resumeMarker types.ResumeMarker, stream WatchStream) error {

	log, err := memstore.OpenLog(w.dbName, true)
	if err != nil {
		return err
	}
	// This goroutine does not leak because cancel is always closed.
	go func() {
		<-cancel

		// Closing the log terminates any ongoing read, and processRequest
		// returns an error.
		log.Close()

		// stream.Send() is automatically cancelled when the call completes,
		// so we don't explicitly cancel sendChanges.

		// TODO(tilaks): cancel processState(), processTransaction().
	}()

	filter, err := newChangeFilter(resumeMarker)
	if err != nil {
		return err
	}

	if isNowResumeMarker(resumeMarker) {
		sendChanges(stream, []types.Change{initialStateSkippedChange})
	}

	// Process initial state.
	store, err := log.ReadState(w.admin)
	if err != nil {
		return err
	}
	st := store.State
	// Save timestamp as processState may modify st.
	timestamp := st.Timestamp()
	changes, err := processor.processState(st)
	if err != nil {
		return err
	}
	if send, err := filter.shouldProcessChanges(timestamp); err != nil {
		return err
	} else if send {
		if err := processChanges(stream, changes, timestamp); err != nil {
			return err
		}
	}

	for {
		// Process transactions.
		mu, err := log.ReadTransaction()
		if err != nil {
			return err
		}
		// Save timestamp as processTransaction may modify mu.
		timestamp := mu.Timestamp
		changes, err = processor.processTransaction(mu)
		if err != nil {
			return err
		}
		if send, err := filter.shouldProcessChanges(timestamp); err != nil {
			return err
		} else if send {
			if err := processChanges(stream, changes, timestamp); err != nil {
				return err
			}
		}
	}
}

// Close implements the service.Watcher interface.
func (w *Watcher) Close() error {
	close(w.closed)
	w.pending.Wait()
	return nil
}

// IsClosed returns true iff the watcher has been closed.
func (w *Watcher) isClosed() bool {
	select {
	case <-w.closed:
		return true
	default:
		return false
	}
}

type changeFilter interface {
	// shouldProcessChanges determines whether to process changes with the given
	// timestamp. Changes should appear in the sequence of the store log, and
	// timestamps should be monotonically increasing.
	shouldProcessChanges(timestamp uint64) (bool, error)
}

type baseFilter struct {
	// initialTimestamp is the minimum timestamp of the first change sent.
	initialTimestamp uint64
	// crossedInitialTimestamp is true if a change with timestamp >=
	// initialTimestamp has already been sent.
	crossedInitialTimestamp bool
}

// onOrAfterFilter accepts any change with timestamp >= initialTimestamp.
type onOrAfterFilter struct {
	baseFilter
}

// onAndAfterFilter accepts any change with timestamp >= initialTimestamp, but
// requires the first change to have timestamp = initialTimestamp.
type onAndAfterFilter struct {
	baseFilter
}

// newChangeFilter creates a changeFilter that processes changes only
// at or after the requested resumeMarker.
func newChangeFilter(resumeMarker []byte) (changeFilter, error) {
	if len(resumeMarker) == 0 {
		return &onOrAfterFilter{baseFilter{0, false}}, nil
	}
	if isNowResumeMarker(resumeMarker) {
		// TODO(tilaks): Get the current resume marker from the log.
		return &onOrAfterFilter{baseFilter{uint64(time.Now().UnixNano()), false}}, nil
	}
	if len(resumeMarker) != 8 {
		return nil, errUnknownResumeMarker
	}
	return &onAndAfterFilter{baseFilter{binary.BigEndian.Uint64(resumeMarker), false}}, nil
}

func (f *onOrAfterFilter) shouldProcessChanges(timestamp uint64) (bool, error) {
	// Bypass checks if a change with timestamp >= initialTimestamp has already
	// been sent.
	if !f.crossedInitialTimestamp {
		if timestamp < f.initialTimestamp {
			return false, nil
		}
	}
	f.crossedInitialTimestamp = true
	return true, nil
}

func (f *onAndAfterFilter) shouldProcessChanges(timestamp uint64) (bool, error) {
	// Bypass checks if a change with timestamp >= initialTimestamp has already
	// been sent.
	if !f.crossedInitialTimestamp {
		if timestamp < f.initialTimestamp {
			return false, nil
		}
		if timestamp > f.initialTimestamp {
			return false, errUnknownResumeMarker
		}
		// TODO(tilaks): if the most recent timestamp in the log is less than
		// initialTimestamp, return ErrUnknownResumeMarker.
	}
	f.crossedInitialTimestamp = true
	return true, nil
}

func processChanges(stream WatchStream, changes []types.Change, timestamp uint64) error {
	addContinued(changes)
	addResumeMarkers(changes, timestampToResumeMarker(timestamp))
	return sendChanges(stream, changes)
}

func sendChanges(stream WatchStream, changes []types.Change) error {
	for _, change := range changes {
		if err := stream.Send(change); err != nil {
			return err
		}
	}
	return nil
}

func addContinued(changes []types.Change) {
	// Last change marks the end of the processed atomic group.
	for i, _ := range changes {
		changes[i].Continued = true
	}
	if len(changes) > 0 {
		changes[len(changes)-1].Continued = false
	}
}

func addResumeMarkers(changes []types.Change, resumeMarker []byte) {
	for i, _ := range changes {
		changes[i].ResumeMarker = resumeMarker
	}
}

func isNowResumeMarker(resumeMarker []byte) bool {
	return bytes.Equal(resumeMarker, nowResumeMarker)
}

func timestampToResumeMarker(timestamp uint64) []byte {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, timestamp)
	return buf
}
