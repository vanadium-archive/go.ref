package watch

import (
	"errors"

	iquery "veyron/services/store/memstore/query"
	"veyron/services/store/memstore/state"

	"veyron2/security"
	"veyron2/services/watch"
	"veyron2/storage"
)

// globProcessor processes log entries into storage entries that match a pattern.
type globProcessor struct {
	// hasProcessedState is true iff the initial state has been processed.
	hasProcessedState bool
	// pid is the identity of the client watching for changes.
	pid security.PublicID
	// path on which the watch is placed. Returned names are rooted at this path.
	path storage.PathName
	// pattern that the returned names match.
	pattern string
	// st is the store state as of the last processed event.
	st *state.State
	// matches is a map of each matching name to the id of the object at that
	// name, as of the last processed event.
	matches map[string]storage.ID
}

func newGlobProcessor(pid security.PublicID, path storage.PathName,
	pattern string) (reqProcessor, error) {

	return &globProcessor{
		hasProcessedState: false,
		pid:               pid,
		path:              path,
		pattern:           pattern,
	}, nil
}

func (p *globProcessor) processState(st *state.State) ([]watch.Change, error) {
	// Check that the initial state has not already been processed.
	if p.hasProcessedState {
		return nil, errors.New("cannot process state after processing the initial state")
	}
	p.hasProcessedState = true

	// Find all names that match the pattern.
	sn := st.MutableSnapshot()
	matches, err := glob(sn, p.pid, p.path, p.pattern)
	if err != nil {
		return nil, err
	}
	p.st = st
	p.matches = matches

	var changes []watch.Change

	// Create a change for every matching name.
	for name, id := range matches {
		cell := sn.Find(id)
		entry := cell.GetEntry()
		change := watch.Change{
			Name:  name,
			State: watch.Exists,
			Value: entry,
		}
		// TODO(tilaks): don't clone change.
		changes = append(changes, change)
	}

	return changes, nil
}

func (p *globProcessor) processTransaction(mus *state.Mutations) ([]watch.Change, error) {
	// Ensure that the initial state has been processed.
	if !p.hasProcessedState {
		return nil, errors.New("cannot process a transaction before processing the initial state")
	}

	previousMatches := p.matches
	// Apply the transaction to the state.
	if err := p.st.ApplyMutations(mus); err != nil {
		return nil, err
	}
	// Find all names that match the pattern in the new state.
	sn := p.st.MutableSnapshot()
	newMatches, err := glob(sn, p.pid, p.path, p.pattern)
	if err != nil {
		return nil, err
	}
	p.matches = newMatches

	var changes []watch.Change

	removed, updated := diffMatches(previousMatches, newMatches, mus.Delta)

	// Create a change for every matching name that was removed.
	for name := range removed {
		change := watch.Change{
			Name:  name,
			State: watch.DoesNotExist,
		}
		// TODO(tilaks): don't clone change
		changes = append(changes, change)
	}

	// Create a change for every matching name that was updated.
	for name := range updated {
		id := newMatches[name]
		cell := sn.Find(id)
		entry := cell.GetEntry()
		change := watch.Change{
			Name:  name,
			State: watch.Exists,
			Value: entry,
		}
		// TODO(tilaks): don't clone change.
		changes = append(changes, change)
	}

	return changes, nil
}

// diffMatches returns the names that have been removed or updated.
//
// A name is removed if it can no longer be resolved, or if the object at that
// name is no longer accessible.
//
// A name is updated if
// 1) it is newly added.
// 2) the object at that name is now accessible.
// 3) the object at the name has a new value or new references.
// 4) the object at that name replaced a previous object.
func diffMatches(previousMatches, newMatches map[string]storage.ID,
	delta map[storage.ID]*state.Mutation) (removed, updated map[string]struct{}) {

	removed = make(map[string]struct{})
	updated = make(map[string]struct{})
	present := struct{}{}

	for name, previousID := range previousMatches {
		if newID, ok := newMatches[name]; !ok {
			// There is no longer an object at this name.
			removed[name] = present
		} else if newID != previousID {
			// The object at this name was replaced.
			updated[name] = present
		}
	}

	for name, newID := range newMatches {
		if _, ok := previousMatches[name]; !ok {
			// An object was added at this name.
			updated[name] = present
			continue
		}
		if _, ok := delta[newID]; ok {
			// The value or implicit directory of the object at this name was
			// updated.
			updated[name] = present
		}
	}

	return
}

// glob returns all names in a snapshot that match a pattern. Each name maps to
// the id of the object in the snapshot at that name.
func glob(sn state.Snapshot, pid security.PublicID, path storage.PathName,
	pattern string) (map[string]storage.ID, error) {

	matches := make(map[string]storage.ID)

	it, err := iquery.GlobIterator(sn, pid, path, pattern)
	if err != nil {
		return nil, err
	}

	for it.IsValid() {
		name := it.Name()
		matchName := append(path, storage.ParsePath(name)...).String()
		entry := it.Get()
		id := entry.Stat.ID
		matches[matchName] = id
		it.Next()
	}

	return matches, nil
}
