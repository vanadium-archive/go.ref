package lib

import (
	"fmt"
	"strings"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/security"

	sample "veyron/examples/wspr_sample"
)

type cacheDispatcher struct {
	cache        interface{}
	errorThrower interface{}
}

func (cd *cacheDispatcher) Lookup(suffix string) (ipc.Invoker, security.Authorizer, error) {
	if strings.HasPrefix(suffix, "errorThrower") {
		return ipc.ReflectInvoker(cd.errorThrower), nil, nil
	}
	return ipc.ReflectInvoker(cd.cache), nil, nil
}

func StartServer(r veyron2.Runtime) (ipc.Server, naming.Endpoint, error) {
	// Create a new server instance.
	s, err := r.NewServer()
	if err != nil {
		return nil, nil, fmt.Errorf("failure creating server: %v", err)
	}

	disp := &cacheDispatcher{
		cache:        sample.NewServerCache(NewCached()),
		errorThrower: sample.NewServerErrorThrower(NewErrorThrower()),
	}

	// Create an endpoint and begin listening.
	endpoint, err := s.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, fmt.Errorf("error listening to service: %v", err)
	}

	// Publish the cache service. This will register it in the mount table and maintain the
	// registration until StopServing is called.
	if err := s.Serve("cache", disp); err != nil {
		return nil, nil, fmt.Errorf("error publishing service: %v", err)
	}
	return s, endpoint, nil
}
