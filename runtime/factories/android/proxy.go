// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package android

import (
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/rpc"
	"v.io/v23/security"

	"v.io/x/ref/runtime/internal/rpc/stream/proxy"
)

// NewProxy creates a new Proxy that listens for network connections on the provided
// (network, address) pair and routes VC traffic between accepted connections.
//
// auth encapsulates the authorization policy of the proxy - which
// servers it is willing to proxy for.
func NewProxy(ctx *context.T, spec rpc.ListenSpec, auth security.Authorizer, names ...string) (shutdown func(), endpoint naming.Endpoint, err error) {
	return proxy.New(ctx, spec, auth, names...)
}
