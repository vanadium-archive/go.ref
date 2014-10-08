package server

import (
	"veyron.io/veyron/veyron2/security"
)

type authorizer struct {
	authFunc remoteAuthFunc
}

func (a *authorizer) Authorize(ctx security.Context) error {
	return a.authFunc(ctx)
}
