package agent

import (
	"v.io/core/veyron2/context"
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/security"
)

func NewUncachedPrincipal(ctx *context.T, fd int, insecureClient ipc.Client) (security.Principal, error) {
	return newUncachedPrincipal(ctx, fd, insecureClient)
}
