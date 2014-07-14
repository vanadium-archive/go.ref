package watch

import (
	"veyron/services/store/memstore/state"
	"veyron2/services/watch"
	"veyron2/verror"
)

var (
	errInitialStateAlreadyProcessed = verror.Internalf("cannot process state after processing the initial state")
	errInitialStateNotProcessed     = verror.Internalf("cannot process a transaction before processing the initial state")
)

// reqProcessor processes log entries into watch changes. At first,
// processState() must be called with the initial state recorded in the log.
// Subsequently, processTransaction() may be called with transactions recorded
// consecutively in the log.
type reqProcessor interface {
	// processState returns a set of changes that represent the initial state of
	// the store. The returned changes need not be the sequence of changes that
	// originally created the initial state (e.g. in the case of compress), but
	// are sufficient to re-construct the state viewable within the request.
	// processState may modify its input.
	processState(st *state.State) ([]watch.Change, error)

	// processTransaction returns the set of changes made in some transaction.
	// The changes are returned in no specific order.
	// processTransaction may modify its input.
	processTransaction(mu *state.Mutations) ([]watch.Change, error)
}
