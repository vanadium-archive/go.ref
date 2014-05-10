// Package schema defines a schema for a movie rating database.  It contains
// Movies, Actors, Parts, etc.
//
// This is designed as a demo application.  The target is that each user would
// have a local copy of the store, and the movie database would be shared
// through a replication group.  User would create new content, movies, reviews,
// etc. and the content would be merged by synchronization.
//
// At the moment, synchronization and queries are not yet implemented, so
// these features are missing.  However, the store provides a basic UI.
package schema

import (
	"veyron2/vom"
)

func init() {
	vom.Register(&Dir{})
	vom.Register(&Movie{})
	vom.Register(&Part{})
	vom.Register(&Person{})
	vom.Register(&Review{})
}
