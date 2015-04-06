// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package stream

import (
	"v.io/v23/verror"
)

const pkgPath = "v.io/x/ref/profiles/internal/rpc/stream"

// The stream family of packages guarantee to return one of the verror codes defined here, their
// messages are constructed so as to avoid embedding a component/method name and are thus
// more suitable for inclusion in other verrors.
var (
	ErrSecurity = verror.Register(pkgPath+".errSecurity", verror.NoRetry, "{:3}")
	ErrNetwork  = verror.Register(pkgPath+".errNetwork", verror.NoRetry, "{:3}")
	ErrProxy    = verror.Register(pkgPath+".errProxy", verror.NoRetry, "{:3}")
	ErrBadArg   = verror.Register(pkgPath+".errBadArg", verror.NoRetry, "{:3}")
	ErrBadState = verror.Register(pkgPath+".errBadState", verror.NoRetry, "{:3}")
	// TODO(cnicolaou): remove this when the rest of the stream sub packages are converted.
	ErrSecOrNet = verror.Register(pkgPath+".errSecOrNet", verror.NoRetry, "{:3}")
	// Update IsStreamError below if you add any other errors here.
)

// IsStreamError returns true if the err is one of the verror codes defined by this package.
func IsStreamError(err error) bool {
	id := verror.ErrorID(err)
	switch id {
	case ErrSecurity.ID, ErrNetwork.ID, ErrProxy.ID, ErrBadArg.ID, ErrBadState.ID, ErrSecOrNet.ID:
		return true
	default:
		return false
	}
}
