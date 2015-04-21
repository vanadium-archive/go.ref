// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build wspr
//
// We restrict to a special build-tag in order to enable
// security.OverrideCaveatValidation, which isn't generally available.

package wsprlib

import (
	"v.io/v23/security"
	"v.io/x/ref/services/wspr/internal/rpc/server"
)

// OverrideCaveatValidation overrides caveat validation.  This must be called in
// order for wspr to be able to delegate caveat validation to javascript.
func OverrideCaveatValidation() {
	security.OverrideCaveatValidation(server.CaveatValidation)
}
