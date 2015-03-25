// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package agent

import (
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
)

func NewUncachedPrincipal(ctx *context.T, fd int, insecureClient rpc.Client) (security.Principal, error) {
	return newUncachedPrincipal(ctx, fd, insecureClient)
}
