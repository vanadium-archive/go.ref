// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package Flow contains a super-interface of the Flow defined in v23/flow/model.go
// to expose information that rpc implementors need to correctly implement the protocol
// (i.e. VOM TypeEncoder, RPC BlessingCache).
//
// TODO(suharshs):  We currently do not want to expose those in the flow API
// because the flow API should be independent of the RPC code, so this is
// our current way of punting on this.
package flow

import (
	"v.io/v23/flow"
)

// Flow is the interface for a flow-controlled channel multiplexed over underlying network connections.
type Flow interface {
	flow.Flow

	// SharedData returns a SharedData cache used for all flows on the underlying authenticated
	// connection.
	SharedData() SharedData
}

// SharedData is a thread-safe store that allows data to be shared across the underlying
// authenticated connection that a flow is multiplexed over.
type SharedData interface {
	// Get returns the 'value' associated with 'key'.
	Get(key interface{}) interface{}

	// GetOrInsert returns the 'value' associated with 'key'. If an entry already exists in the
	// cache with the 'key', the 'value' is returned, otherwise 'create' is called to create a new
	// value N, the cache is updated, and N is returned.  GetOrInsert may be called from
	// multiple goroutines concurrently.
	GetOrInsert(key interface{}, create func() interface{}) interface{}
}
