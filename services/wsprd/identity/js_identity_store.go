package identity

import "veyron.io/veyron/veyron2/security"
import "sync"

// JSPublicIDHandles is a store for PublicIDs in use by JS code.
// We don't pass the full PublicID to avoid serializing and deserializing a
// potentially huge forest of blessings.  Instead we pass to JS a handle to a public
// identity and have all operations involve cryptographic operations call into go.
type JSPublicIDHandles struct {
	mu         sync.Mutex
	lastHandle int64
	store      map[int64]security.PublicID
}

// NewJSPublicIDHandles returns a newly initialized JSPublicIDHandles
func NewJSPublicIDHandles() *JSPublicIDHandles {
	return &JSPublicIDHandles{
		store: map[int64]security.PublicID{},
	}
}

// Add adds a PublicID to the store and returns the handle to it.
func (s *JSPublicIDHandles) Add(identity security.PublicID) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastHandle++
	handle := s.lastHandle
	s.store[handle] = identity
	return handle
}

// Remove removes the PublicID associated with the handle.
func (s *JSPublicIDHandles) Remove(handle int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.store, handle)
}

// Get returns the PublicID represented by the handle.  Returns nil
// if no PublicID exists for the handle.
func (s *JSPublicIDHandles) Get(handle int64) security.PublicID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store[handle]
}
