package server

import (
	"v.io/veyron/veyron2/security"
)

type authorizer struct {
	authFunc remoteAuthFunc
}

func (a *authorizer) Authorize(ctx security.Context) error {
	return a.authFunc(ctx)
}
