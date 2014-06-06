package ipc

import (
	"veyron2/context"
)

// contextImpl implements the context.T interface.
// TODO(mattr): Consider moving this to a separate package to mirror the layout of the
// interfaces in veyron2.
type contextImpl struct{}

// InternalNewContext creates a new context.T.  This function should only
// be called from within the runtime implementation.
func InternalNewContext() context.T {
	return &contextImpl{}
}
