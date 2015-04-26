// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/security"
)

// These are the internal functions only for use in the proxy_test package.

func InternalNew(rid naming.RoutingID, ctx *context.T, auth security.Authorizer) (*Proxy, func(), naming.Endpoint, error) {
	proxy, err := internalNew(rid, ctx, v23.GetListenSpec(ctx), auth)
	if err != nil {
		return nil, nil, nil, err
	}
	return proxy, proxy.shutdown, proxy.endpoint(), err
}

func NumProcesses(proxy *Proxy) int {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	return len(proxy.processes)
}
