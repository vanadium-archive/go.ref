// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package namespace

import (
	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security/access"
	"v.io/x/lib/vlog"
	"v.io/x/ref/lib/apilog"
)

// setPermsInMountTable sets the Permissions in a single server.
func setPermsInMountTable(ctx *context.T, client rpc.Client, name string, perms access.Permissions, version, id string, opts []rpc.CallOpt) (s status) {
	s.id = id
	ctx = withTimeout(ctx)
	s.err = client.Call(ctx, name, "SetPermissions", []interface{}{perms, version}, nil, append(opts, options.NoResolve{})...)
	return
}

func (ns *namespace) SetPermissions(ctx *context.T, name string, perms access.Permissions, version string, opts ...naming.NamespaceOpt) error {
	defer apilog.LogCallf(ctx, "name=%.10s...,perms=,version=%.10s...,opts...=%v", name, version, opts)(ctx, "") // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	client := v23.GetClient(ctx)

	// Apply to all mount tables implementing the name.
	f := func(ctx *context.T, mt, id string) status {
		return setPermsInMountTable(ctx, client, mt, perms, version, id, getCallOpts(opts))
	}
	err := ns.dispatch(ctx, name, f, opts)
	vlog.VI(1).Infof("SetPermissions(%s, %v, %s) -> %v", name, perms, version, err)
	return err
}

// GetPermissions gets Permissions from a mount table.
func (ns *namespace) GetPermissions(ctx *context.T, name string, opts ...naming.NamespaceOpt) (perms access.Permissions, version string, err error) {
	defer apilog.LogCallf(ctx, "name=%.10s...,opts...=%v", name, opts)(ctx, "perms=,version=%.10s...,err=%v", &version, &err) // gologcop: DO NOT EDIT, MUST BE FIRST STATEMENT
	client := v23.GetClient(ctx)

	// Resolve to all the mount tables implementing name.
	me, rerr := ns.ResolveToMountTable(ctx, name, opts...)
	if rerr != nil {
		err = rerr
		return
	}
	mts := me.Names()

	call, serr := ns.parallelStartCall(ctx, client, mts, "GetPermissions", []interface{}{}, getCallOpts(opts))
	if serr != nil {
		err = serr
		return
	}
	err = call.Finish(&perms, &version)
	return
}
