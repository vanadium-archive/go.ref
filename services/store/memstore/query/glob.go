package query

import (
	"veyron/lib/glob"
	"veyron/services/store/memstore/refs"
	"veyron/services/store/memstore/state"

	"veyron2/security"
	"veyron2/storage"
)

type globIterator struct {
	state.Iterator
	pathLen int
	glob    *glob.Glob
}

// GlobStream represents a sequence of results from a glob call.
type GlobStream interface {
	// IsValid returns true iff the iterator refers to an element.
	IsValid() bool

	// Return one possible name for this entry.
	Name() string

	// Next advances to the next element.
	Next()
}

// Glob returns an iterator that emits all values that match the given pattern.
func Glob(sn state.Snapshot, clientID security.PublicID, path storage.PathName, pattern string) (GlobStream, error) {
	return GlobIterator(sn, clientID, path, pattern)
}

// GlobIterator returns an iterator that emits all values that match the given pattern.
func GlobIterator(sn state.Snapshot, clientID security.PublicID, path storage.PathName, pattern string) (state.Iterator, error) {
	parsed, err := glob.Parse(pattern)
	if err != nil {
		return nil, err
	}

	g := &globIterator{
		pathLen: len(path),
		glob:    parsed,
	}
	g.Iterator = sn.NewIterator(clientID, path, state.ListPaths, state.IterFilter(g.filter))

	return g, nil
}

func (g *globIterator) filter(parent *refs.FullPath, path *refs.Path) (bool, bool) {
	// We never get to a path unless we've first approved its parent.
	// We can therefore only check a suffix of the glob pattern.
	prefixLen := parent.Len() - g.pathLen
	matched, suffix := g.glob.PartialMatch(prefixLen, []string(path.Name()))
	return matched && suffix.Len() == 0, matched
}
