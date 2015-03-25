package server

import (
	"v.io/v23/context"
)

type authorizer struct {
	authFunc remoteAuthFunc
}

func (a *authorizer) Authorize(ctx *context.T) error {
	return a.authFunc(ctx)
}
