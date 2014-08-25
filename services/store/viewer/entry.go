package viewer

import (
	"path/filepath"
	"sort"
	"time"

	"veyron2/context"
	"veyron2/naming"
	"veyron2/storage"
)

// Entry is the type used to pass store values to user-defined templates.
type Entry struct {
	ctx       context.T
	storeRoot string
	store     storage.Store
	Name      string
	Value     interface{}
}

// EntryForRawTemplate is the type used to pass store values to the "raw"
// template defined in viewer.go.
type EntryForRawTemplate struct {
	*Entry
	Subdirs    []string // relative names
	RawSubdirs bool     // whether to add "?raw" to subdir hrefs
}

// abspath returns the absolute path from a path relative to this value.
func (e *Entry) abspath(path string) string {
	return naming.Join(e.storeRoot, e.Name, path)
}

// Date performs a Time conversion, given an integer argument that represents a
// time in nanoseconds since the Unix epoch.
func (e *Entry) Date(ns int64) time.Time {
	return time.Unix(0, ns)
}

// Join joins the path elements.
func (e *Entry) Join(elem ...string) string {
	return filepath.ToSlash(filepath.Join(elem...))
}

// Base returns the last element of the path.
func (e *Entry) Base(path string) string {
	return filepath.Base(path)
}

// Glob performs a glob expansion of the pattern.  The results are sorted.
func (e *Entry) Glob(pattern string) ([]string, error) {
	results := e.store.Bind(e.abspath("")).Glob(e.ctx, pattern)
	names := []string{}
	rStream := results.RecvStream()
	for rStream.Advance() {
		names = append(names, rStream.Value())
	}
	if err := rStream.Err(); err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

// Get fetches a value from the store, where path is relative to this value.
// The result is nil if the value does not exist.
func (e *Entry) Get(path string) interface{} {
	en, err := e.store.Bind(e.abspath(path)).Get(e.ctx)
	if err != nil {
		return nil
	}
	return en.Value
}
