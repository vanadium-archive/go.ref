package agent

import (
	"v.io/v23/context"
	"v.io/v23/ipc"
	"v.io/v23/security"
)

func NewUncachedPrincipal(ctx *context.T, fd int, insecureClient ipc.Client) (security.Principal, error) {
	return newUncachedPrincipal(ctx, fd, insecureClient)
}
