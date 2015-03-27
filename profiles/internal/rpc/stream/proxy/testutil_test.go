// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"v.io/v23/naming"
	"v.io/v23/security"
)

// These are the internal functions only for use in the proxy_test package.

func InternalNew(rid naming.RoutingID, p security.Principal, net, addr, pubAddr string) (*Proxy, func(), naming.Endpoint, error) {
	proxy, err := internalNew(rid, p, net, addr, pubAddr)
	return proxy, proxy.shutdown, proxy.endpoint(), err
}

func NumProcesses(proxy *Proxy) int {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	return len(proxy.processes)
}
