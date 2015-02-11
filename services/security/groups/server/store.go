package server

import "fmt"

// Store is a key-value store that uses etags for optimistic concurrency
// control. The etags passed to Update and Delete must come from Get. If in the
// meantime some client has called Update or Delete on the same key, the etag
// will be stale and the method call will fail.
//
// Note, this API disallows empty etags to simplify implementation. The group
// server is the only client of this API and always specifies etags.
type Store interface {
	// Fails if the given key is unknown (ErrUnknownKey).
	Get(k string) (v interface{}, etag string, err error)

	// Fails if an entry already exists for the given key (ErrKeyAlreadyExists).
	Insert(k string, v interface{}) error

	// Fails if the given key is unknown (ErrUnknownKey).
	// Fails if etag doesn't match (ErrBadEtag).
	Update(k string, v interface{}, etag string) error

	// Fails if the given key is unknown (ErrUnknownKey).
	// Fails if etag doesn't match (ErrBadEtag).
	Delete(k string, etag string) error
}

////////////////////////////////////////
// Store error types

type ErrUnknownKey struct {
	Key string
}

func (err *ErrUnknownKey) Error() string {
	return "unknown key: " + err.Key
}

type ErrKeyAlreadyExists struct {
	Key string
}

func (err *ErrKeyAlreadyExists) Error() string {
	return "key already exists: " + err.Key
}

type ErrBadEtag struct {
	OldEtag string
	NewEtag string
}

func (err *ErrBadEtag) Error() string {
	return fmt.Sprintf("etag is out of date: %s != %s", err.OldEtag, err.NewEtag)
}
