package ipc

import (
	"v.io/v23/ipc"
)

func InternalServerResolveToEndpoint(s ipc.Server, name string) (string, error) {
	return s.(*server).resolveToEndpoint(name)
}
