// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interfaces

import (
	"v.io/v23/context"
	"v.io/x/ref/services/syncbase/store"
)

// Table is an internal interface to the table layer.
type Table interface {
	// Database returns the database handle for this table.
	Database() Database

	// UpdatePrefixPermsIndexForSet updates the prefix permissions index to
	// reflect SetPrefixPermissions on the given key.
	UpdatePrefixPermsIndexForSet(ctx *context.T, tx store.Transaction, key string) error

	// UpdatePrefixPermsIndexForDelete updates the prefix permissions index to
	// reflect DeletePrefixPermissions on the given key.
	UpdatePrefixPermsIndexForDelete(ctx *context.T, tx store.Transaction, key string) error

	// Name returns the name of this table.
	Name() string
}
