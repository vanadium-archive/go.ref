// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interfaces

import (
	"v.io/syncbase/x/ref/services/syncbase/store"
	"v.io/v23/context"
	"v.io/v23/rpc"
)

// Service is an internal interface to the service layer.
// All methods return VDL-compatible errors.
type Service interface {
	// St returns the storage engine instance for this service.
	St() store.Store

	// App returns the App with the specified name.
	App(ctx *context.T, call rpc.ServerCall, appName string) (App, error)
}
