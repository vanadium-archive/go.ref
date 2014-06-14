package lib

import (
	"fmt"

	"veyron2"
	"veyron2/ipc"
	"veyron2/naming"
	"veyron2/security"

	sample "veyron/examples/wspr_sample"
)

type cacheDispatcher struct {
	cached interface{}
}

func (cd *cacheDispatcher) Lookup(string) (ipc.Invoker, security.Authorizer, error) {
	return ipc.ReflectInvoker(cd.cached), nil, nil
}

func StartServer(r veyron2.Runtime) (ipc.Server, naming.Endpoint, error) {
	// Create a new server instance.
	s, err := r.NewServer()
	if err != nil {
		return nil, nil, fmt.Errorf("failure creating server: %v", err)
	}

	// Register the "cache" prefix with the cache dispatcher.
	serverCache := sample.NewServerCache(NewCached())
	if err := s.Register("cache", &cacheDispatcher{cached: serverCache}); err != nil {
		return nil, nil, fmt.Errorf("error registering cache service: %v", err)
	}

	// Register the "errorthrower" prefix with the errorthrower dispatcher.
	errorThrower := sample.NewServerErrorThrower(NewErrorThrower())
	if err := s.Register("errorthrower", &cacheDispatcher{cached: errorThrower}); err != nil {
		return nil, nil, fmt.Errorf("error registering error thrower service: %v", err)
	}

	// Create an endpoint and begin listening.
	endpoint, err := s.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, fmt.Errorf("error listening to service: %v", err)
	}

	// Publish the cache service. This will register it in the mount table and maintain the
	// registration until StopServing is called.
	if err := s.Publish("cache"); err != nil {
		return nil, nil, fmt.Errorf("error publishing service: %v", err)
	}

	return s, endpoint, nil
}
