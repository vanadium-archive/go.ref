package testutil

import (
	"v.io/core/veyron2/ipc"
	"v.io/core/veyron2/security"
)

// LeafDispatcher returns a dispatcher for a single object obj, using
// ReflectInvokerOrDie to invoke methods. Lookup only succeeds on the empty
// suffix.  The provided auth is returned for successful lookups.
func LeafDispatcher(obj interface{}, auth security.Authorizer) ipc.Dispatcher {
	return &leafDispatcher{ipc.ReflectInvokerOrDie(obj), auth}
}

type leafDispatcher struct {
	invoker ipc.Invoker
	auth    security.Authorizer
}

func (d leafDispatcher) Lookup(suffix string) (interface{}, security.Authorizer, error) {
	if suffix != "" {
		return nil, nil, ipc.MakeUnknownSuffix(nil, suffix)
	}
	return d.invoker, d.auth, nil
}
