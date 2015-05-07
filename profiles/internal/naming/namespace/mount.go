// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package namespace

import (
	"time"

	"v.io/v23"
	"v.io/v23/context"
	"v.io/v23/naming"
	"v.io/v23/options"
	"v.io/v23/rpc"
	"v.io/v23/security"
	"v.io/x/lib/vlog"
)

// mountIntoMountTable mounts a single server into a single mount table.
func mountIntoMountTable(ctx *context.T, client rpc.Client, name, server string, ttl time.Duration, flags naming.MountFlag, id string, opts ...rpc.CallOpt) (s status) {
	s.id = id
	ctx = withTimeout(ctx)
	s.err = client.Call(ctx, name, "Mount", []interface{}{server, uint32(ttl.Seconds()), flags}, nil, append(opts, options.NoResolve{})...)
	return
}

// Mount implements Namespace.Mount.
func (ns *namespace) Mount(ctx *context.T, name, server string, ttl time.Duration, opts ...naming.NamespaceOpt) error {
	defer vlog.LogCall()()

	var flags naming.MountFlag
	for _, o := range opts {
		// NB: used a switch since we'll be adding more options.
		switch v := o.(type) {
		case naming.ReplaceMount:
			if v {
				flags |= naming.MountFlag(naming.Replace)
			}
		case naming.ServesMountTable:
			if v {
				flags |= naming.MountFlag(naming.MT)
			}
		case naming.IsLeaf:
			if v {
				flags |= naming.MountFlag(naming.Leaf)
			}
		}
	}

	client := v23.GetClient(ctx)
	// Mount the server in all the returned mount tables.
	f := func(ctx *context.T, mt, id string) status {
		return mountIntoMountTable(ctx, client, mt, server, ttl, flags, id, getCallOpts(opts)...)
	}
	err := ns.dispatch(ctx, name, f, opts)
	vlog.VI(1).Infof("Mount(%s, %q) -> %v", name, server, err)
	return err
}

// unmountFromMountTable removes a single mounted server from a single mount table.
func unmountFromMountTable(ctx *context.T, client rpc.Client, name, server string, id string, opts ...rpc.CallOpt) (s status) {
	s.id = id
	ctx = withTimeout(ctx)
	s.err = client.Call(ctx, name, "Unmount", []interface{}{server}, nil, append(opts, options.NoResolve{})...)
	return
}

// Unmount implements Namespace.Unmount.
func (ns *namespace) Unmount(ctx *context.T, name, server string, opts ...naming.NamespaceOpt) error {
	defer vlog.LogCall()()
	// Unmount the server from all the mount tables.
	client := v23.GetClient(ctx)
	f := func(ctx *context.T, mt, id string) status {
		return unmountFromMountTable(ctx, client, mt, server, id, getCallOpts(opts)...)
	}
	err := ns.dispatch(ctx, name, f, opts)
	vlog.VI(1).Infof("Unmount(%s, %s) -> %v", name, server, err)
	return err
}

// deleteFromMountTable deletes a name from a single mount table.  If there are any children
// and deleteSubtree isn't true, nothing is deleted.
func deleteFromMountTable(ctx *context.T, client rpc.Client, name string, deleteSubtree bool, id string, opts ...rpc.CallOpt) (s status) {
	s.id = id
	ctx = withTimeout(ctx)
	s.err = client.Call(ctx, name, "Delete", []interface{}{deleteSubtree}, nil, append(opts, options.NoResolve{})...)
	return
}

// RDeleteemove implements Namespace.Delete.
func (ns *namespace) Delete(ctx *context.T, name string, deleteSubtree bool, opts ...naming.NamespaceOpt) error {
	defer vlog.LogCall()()
	// Remove from all the mount tables.
	client := v23.GetClient(ctx)
	f := func(ctx *context.T, mt, id string) status {
		return deleteFromMountTable(ctx, client, mt, deleteSubtree, id, getCallOpts(opts)...)
	}
	err := ns.dispatch(ctx, name, f, opts)
	vlog.VI(1).Infof("Remove(%s, %v) -> %v", name, deleteSubtree, err)
	return err
}

func str2pattern(strs []string) (ret []security.BlessingPattern) {
	ret = make([]security.BlessingPattern, len(strs))
	for i, s := range strs {
		ret[i] = security.BlessingPattern(s)
	}
	return
}
