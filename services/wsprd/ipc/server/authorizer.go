package server

import (
	"v.io/v23/security"
)

type authorizer struct {
	authFunc remoteAuthFunc
}

func (a *authorizer) Authorize(call security.Call) error {
	return a.authFunc(call)
}
