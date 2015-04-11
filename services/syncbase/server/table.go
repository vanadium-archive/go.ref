// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build ignore

package server

import "v.io/syncbase/v23/services/syncbase"

type table struct {
	name string
	d    *database
}

var _ syncbase.TableServerMethods = (*table)(nil)

// TODO(sadovsky): Implement Glob.

////////////////////////////////////////
// Internal helpers

func (t *table) keyPart() string {
	return joinKeyParts(t.d.keyPart(), t.name)
}
