package proxy

import (
	"v.io/v23/naming"
	"v.io/v23/security"
)

// This exprts the internalNew function only for use in the proxy_test package.
func InternalNew(rid naming.RoutingID, p security.Principal, net, addr, pubAddr string) (func(), naming.Endpoint, error) {
	return internalNew(rid, p, net, addr, pubAddr)
}
