// Copyright 2016 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"v.io/v23/context"
	"v.io/v23/rpc"
	"v.io/v23/security"
)

type allocatorImpl struct{}

// Create creates a new instance of the service. The instance's
// blessings will be an extension of the blessings granted on this RPC.
// It returns the object name of the new instance.
func (i *allocatorImpl) Create(ctx *context.T, call rpc.ServerCall) (string, error) {
	b, _ := security.RemoteBlessingNames(ctx, call.Security())
	ctx.Infof("Create() called by %v", b)
	return "", nil
}

// Delete deletes the instance with the given name.
func (i *allocatorImpl) Delete(ctx *context.T, call rpc.ServerCall, name string) error {
	b, _ := security.RemoteBlessingNames(ctx, call.Security())
	ctx.Infof("Delete(%q) called by %v", name, b)
	return nil
}

// List returns a list of all the instances owned by the caller.
func (i *allocatorImpl) List(ctx *context.T, call rpc.ServerCall) ([]string, error) {
	b, _ := security.RemoteBlessingNames(ctx, call.Security())
	ctx.Infof("List() called by %v", b)
	return nil, nil
}
