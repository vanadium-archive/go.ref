package rpc

import (
	"v.io/v23/rpc"
)

func InternalServerResolveToEndpoint(s rpc.Server, name string) (string, error) {
	return s.(*server).resolveToEndpoint(name)
}
