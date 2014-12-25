package ipc

import (
	"v.io/veyron/veyron2/ipc"
)

func InternalServerResolveToEndpoint(s ipc.Server, name string) (string, error) {
	return s.(*server).resolveToEndpoint(name)
}
