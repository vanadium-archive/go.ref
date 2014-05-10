package query

import (
	"veyron/lib/glob"
	"veyron/services/store/memstore/refs"
	"veyron/services/store/memstore/state"
	"veyron/services/store/service"

	"veyron2/security"
	"veyron2/storage"
)

type globIterator struct {
	state.Iterator
	pathLen int
	glob    *glob.Glob
}

// Glob returns an iterator that emits all values that match the given pattern.
func Glob(sn state.Snapshot, clientID security.PublicID, path storage.PathName, pattern string) (service.GlobStream, error) {
	parsed, err := glob.Parse(pattern)
	if err != nil {
		return nil, err
	}

	g := &globIterator{
		pathLen: len(path),
		glob:    parsed,
	}
	g.Iterator = sn.NewIterator(clientID, path, state.IterFilter(g.filter))

	return g, nil
}

func (g *globIterator) filter(parent *refs.FullPath, path *refs.Path) (bool, bool) {
	// We never get to a path unless we've first approved its parent.
	// We can therefore only check a suffix of the glob pattern.
	prefixLen := parent.Len() - g.pathLen
	matched, suffix := g.glob.PartialMatch(prefixLen, []string(path.Name()))
	return matched && suffix.Len() == 0, matched
}

// Name returns names relative to the object that began the glob.
func (g *globIterator) Name() string {
	name := g.Iterator.Name()
	skip := g.pathLen
	if skip == 0 {
		return name
	}

	for i, r := range name {
		if r == '/' {
			if skip--; skip == 0 {
				return name[i+1:]
			}
		}
	}
	// If we didn't find all the slashes already that means that we should skip even
	// the last path segment.
	return ""
}
