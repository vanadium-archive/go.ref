package static

import (
	"v.io/v23/context"
	"v.io/v23/naming"

	"v.io/x/ref/profiles/internal/ipc/stream/proxy"
)

// NewProxy creates a new Proxy that listens for network connections on the provided
// (network, address) pair and routes VC traffic between accepted connections.
func NewProxy(ctx *context.T, network, address, pubAddress string, names ...string) (shutdown func(), endpoint naming.Endpoint, err error) {
	return proxy.New(ctx, network, address, pubAddress, names...)
}
