// Package memstore implements an in-memory version of the veyron2/storage API.
// The store is logged to a file, and it is persistent, but the entire state is
// also kept in memory at all times.
//
// The purpose of this fake is to explore the model.  It is for testing; it
// isn't intended to be deployed.
package memstore
