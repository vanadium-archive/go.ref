package agent

import (
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
)

func NewUncachedPrincipal(ctx *context.T, fd int, insecureClient rpc.Client) (security.Principal, error) {
	return newUncachedPrincipal(ctx, fd, insecureClient)
}
