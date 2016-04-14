// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package client defines an interface that syncbase clients must implement.
// Each syncbase instance in a longevity test will have an associated client
// that interacts with it.  The clients' behavior should reflect real-world use
// cases.
package client

import (
	"v.io/v23/context"
	wire "v.io/v23/services/syncbase"
)

// Client represents a syncbase client in a longevity test.
type Client interface {
	// String returns a string representation of the client, suitable for
	// printing.
	String() string

	// Run implements the main client logic.  It is the implementation's
	// responsibility to guarantee that the database and collections exist.
	// Run must return when the stop channel is closed.
	Run(ctx *context.T, sbName, dbName string, collectionIds []wire.Id, stop <-chan struct{}) error
}
