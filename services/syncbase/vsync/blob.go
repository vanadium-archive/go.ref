// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vsync

import (
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/verror"
)

//////////////////////////////////////////////////
// Methods for blob fetch between Syncbases.

func (s *syncService) FetchBlob(ctx *context.T, call rpc.ServerCall) error {
	return verror.NewErrNotImplemented(ctx)
}
