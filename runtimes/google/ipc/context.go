package ipc

import (
	"veyron2/ipc"
)

// context implements the ipc.ServerContext interface.
type context struct{}

// InternalNewContext creates a new ipc.Context.  This function should only
// be called from within the runtime implementation.
func InternalNewContext() ipc.Context {
	return &context{}
}
